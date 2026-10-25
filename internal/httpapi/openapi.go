package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
)

// Placeholders substituted while the document is assembled. The catalogue below
// stays valid, copy-pasteable JSON so the examples in /docs read exactly like
// the requests they send, while the generated values keep them executable.
const (
	skinTexturePlaceholder = "{{skinTexture}}"
	mapPixelsPlaceholder   = "{{mapPixels}}"
)

// openAPIDocument is built once at startup; /api/v1/openapi.json and the Scalar
// page therefore always describe the same contract.
var openAPIDocument = buildOpenAPIDocument()

func buildOpenAPIDocument() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Minecraft View Generator API", "version": model.SupportedVersion, "description": "Minecraft 1.21.11 rendering API backed by a Java 21 resource-aware renderer. The service returns PNG or base64 PNG and fails explicitly when resources are unavailable."},
		"paths": map[string]any{
			"/api/v1/versions":                map[string]any{"get": map[string]any{"summary": "List local resource contexts", "description": "存活探针也读这个接口：它不需要 API key，恒返回 200 与版本列表。", "responses": map[string]any{"200": map[string]any{"description": "Version states"}}}},
			"/api/v1/resources/{version}":     map[string]any{"parameters": []any{map[string]any{"name": "version", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}}, "put": map[string]any{"summary": "Prepare resources", "responses": map[string]any{"202": map[string]any{"description": "Preparation started"}, "200": map[string]any{"description": "Already ready"}}}, "get": map[string]any{"summary": "Read preparation state", "responses": map[string]any{"200": map[string]any{"description": "Resource state"}, "404": map[string]any{"description": "Unknown version"}}}},
			"/api/v1/resources/sources":       sourcesOperation(),
			"/api/v1/resources/packs":         resourcePacksOperation(),
			"/api/v1/resources/custom/reload": reloadOperation(),
			"/api/v1/render":                  renderOperation(),
		},
		"components": map[string]any{"schemas": componentSchemas()},
	}
}

// componentSchemas assembles the navigable half of the contract: the request envelope, one schema per
// data structure a render type decodes, and the error payload. The field tables mirror the
// "请求参数总表" section of docs/api.md; internal/model/docs_test.go fails when the two drift apart.
func componentSchemas() map[string]any {
	schemas := dataSchemas()
	schemas["RenderRequest"] = renderRequestSchema()
	schemas["ResourcePackReport"] = resourcePackReportSchema()
	schemas["ResourcePack"] = resourcePackSchema()
	schemas["ResourcePackOrder"] = resourcePackOrderSchema()
	schemas["Error"] = map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{"error": map[string]any{"type": "object", "required": []string{"code", "message"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}}}}
	return schemas
}

// resourcePackReportSchema is the body of GET /api/v1/resources/packs and the resourcePacks segment
// of the sources report. The order of packs is the load order, not a set.
func resourcePackReportSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"directory", "status", "packs"},
		"properties": map[string]any{
			"directory": field("string", "读取的材质包目录：RESOURCE_PACK_ROOT，缺省为 <DATA_DIR>/resourcepacks（与 Minecraft 的目录名一致），启动时不存在会自动创建。", nil),
			"cacheRoot": field("string", "zip 的解包缓存目录（内容哈希寻址），可整目录删除，服务重启后会按需重建。", nil),
			"status":    field("string", "ready 表示目录可读；error 表示目录无法创建或读取，原因见 error，此时保留上一份可渲染的包栈。", map[string]any{"enum": []string{"ready", "error"}}),
			"error":     field("string", "目录级错误，仅 status=error 时出现。", nil),
			"packs": map[string]any{
				"type":        "array",
				"description": "按加载顺序排列：第 1 项最先加载、被后面的覆盖，最后一项优先级最高。",
				"items":       schemaRef("ResourcePack"),
			},
			"warnings":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "被忽略的条目等非致命提示，上限 64 条。"},
			"updatedAt": field("string", "本次扫描完成的时间（RFC 3339）。", map[string]any{"format": "date-time"}),
		},
	}
}

func resourcePackSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"file", "priority", "status"},
		"properties": map[string]any{
			"file":       field("string", "目录内的压缩包文件名，也是设置优先级时使用的标识。", nil),
			"priority":   field("integer", "加载顺序序号，从 1 开始；数值越大越晚加载，因此覆盖越多的包。status=failed 的条目该值无实际意义。", map[string]any{"minimum": 1}),
			"addedAt":    field("string", "本服务首次发现该包的时间（RFC 3339）。", map[string]any{"format": "date-time"}),
			"root":       field("string", "传给渲染器的解包目录绝对路径；status=failed 时为空。", nil),
			"overlays":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "按叠加顺序生效的 overlay 目录名（相对 root）。pack.mcmeta 的 overlays.entries 中覆盖当前渲染版本格式（1.21.11 → 75）的条目才出现，它们依次覆盖包根；目录不存在或指到包外的条目被忽略。未声明 overlays 时该字段不出现。"},
			"files":      field("integer", "压缩包内的条目数。", nil),
			"packFormat": field("integer", "pack.mcmeta 声明的 pack_format；只声明 min_format/max_format 时取其下界。支持多种写法：数字、[major, minor] 数组，supported_formats 的对象或 [min, max] 数组。", nil),
			"status":     field("string", "loaded 表示参与渲染；failed 表示包损坏或格式不兼容，原因见 error，不影响其余包。", map[string]any{"enum": []string{"loaded", "failed"}}),
			"error":      field("string", "该包被拒绝的原因，仅 status=failed 时出现。", nil),
		},
	}
}

func resourcePackOrderSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"order"},
		"properties": map[string]any{
			"order": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "新的加载顺序：必须恰好包含 GET /api/v1/resources/packs 里每个 file 一次。数组第 1 项最先加载、被后面的覆盖。",
			},
		},
	}
}

func renderRequestSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"version", "type", "output", "data"},
		"properties": map[string]any{
			"version": map[string]any{"type": "string", "const": model.SupportedVersion, "description": "固定为 1.21.11，其他版本返回 400 invalid_data。"},
			"type":    map[string]any{"type": "string", "enum": model.SupportedTypes(), "description": "渲染类型，决定 data 的结构。"},
			"output":  map[string]any{"type": "string", "enum": []string{model.OutputPNG, model.OutputBase64}, "description": "png 直接返回 image/png；base64 返回 {mimeType, encoding, data, width, height}。"},
			"data": map[string]any{
				"description": dataDescription(),
				"oneOf": []any{
					schemaRef("Item"), schemaRef("TooltipData"), schemaRef("ContainerData"),
					schemaRef("SkinData"), schemaRef("PlayerInventoryData"), schemaRef("PlayerListData"),
					schemaRef("MapData"), schemaRef("AchievementData"),
				},
			},
			"options": optionsSchema(),
		},
	}
}

// dataDescription is the type → schema map rendered into the Scalar page, so a caller that only reads
// the OpenAPI document still learns which structure each type takes and which fields are required.
func dataDescription() string {
	return strings.Join([]string{
		"data 的结构由 type 决定，逐字段说明见 docs/api.md 的「请求参数总表」。",
		"",
		"| type | data 结构 |",
		"| --- | --- |",
		"| item | Item |",
		"| tooltip | TooltipData（item 为 Item） |",
		"| inventory / item_container / enderchest | ContainerData |",
		"| playerInventory | PlayerInventoryData |",
		"| skin | SkinData |",
		"| playerlist | PlayerListData（players[] 为 PlayerListEntry） |",
		"| map | MapData（markers[] 为 MapMarker） |",
		"| achievement | AchievementData（icon 为 Item） |",
		"",
		"必填：item.id；tooltip 的 item.id 与 title 至少一个；item_container.container；skin.texture；playerInventory.skin.texture；playerlist 每个玩家的 texture 与 name/display 之一；achievement.title 与 icon.id。",
		"data 内的未知字段被静默忽略（不报错也不生效）；顶层字段与 options 内的未知字段由 DisallowUnknownFields 拒绝，返回 400 invalid_json。",
	}, "\n")
}

func optionsSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "画布尺寸、放大倍率与外观。width/height 是画布（渲染分辨率）；scale 是成品图的放大倍率。background 与 transparent 只对 item 有效（其余九种类型自绘不透明背景）。",
		"properties": map[string]any{
			"width":       field("integer", "画布宽度。只给宽度时高度按该类型的原生画布比例换算，不拉伸。", map[string]any{"minimum": 16, "maximum": 1600}),
			"height":      field("integer", "画布高度。只给高度时宽度同样按原生比例换算。", map[string]any{"minimum": 16, "maximum": 1200}),
			"scale":       scaleSchema(),
			"background":  field("string", "以 #RRGGBB 指定的画布底色，只对 item 有效：先铺满整张画布，图标自身的透明像素会透出这个颜色。缺省透明，格式不对按透明处理。", map[string]any{"pattern": "^#?[0-9a-fA-F]{6}$"}),
			"transparent": field("boolean", "只对 item 有效：保留图标自身的透明像素并忽略 background。", map[string]any{"default": false}),
		},
	}
}

func scaleSchema() map[string]any {
	return map[string]any{
		"description": "成品图的放大倍率。数字为固定倍率（0 与 1 表示不放大，支持小数，上限 16）；对象可用 ratio，或用 width/height 给出目标最终尺寸并按画布等比取倍率。",
		"oneOf": []any{
			map[string]any{"type": "number", "minimum": 0, "maximum": model.MaxScaleRatio},
			map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
				"ratio":  map[string]any{"type": "number", "minimum": 0, "maximum": model.MaxScaleRatio},
				"width":  map[string]any{"type": "integer", "minimum": 16},
				"height": map[string]any{"type": "integer", "minimum": 16},
			}},
		},
	}
}

// dataSchemas returns the structures a render type decodes. The names are the exported Go types, which
// is also how docs/api.md refers to them.
func dataSchemas() map[string]any {
	return map[string]any{
		"Item":                itemSchema(),
		"Slot":                slotSchema(),
		"ContainerData":       containerDataSchema(),
		"TooltipData":         tooltipDataSchema(),
		"SkinData":            skinDataSchema(),
		"PlayerInventoryData": playerInventoryDataSchema(),
		"PlayerListEntry":     playerListEntrySchema(),
		"PlayerListData":      playerListDataSchema(),
		"MapMarker":           mapMarkerSchema(),
		"MapData":             mapDataSchema(),
		"AchievementData":     achievementDataSchema(),
	}
}

func itemSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "一件物品。item、tooltip.item、slots[].item、armor[]、mainHand/offhand、achievement.icon 共用这套字段。",
		"required":    []string{"id"},
		"properties": map[string]any{
			"id":              field("string", "物品 id，如 minecraft:diamond_sword。物品位置必填，缺失返回 400 invalid_data。", nil),
			"count":           field("integer", "堆叠数量，默认 0。只在容器 / 背包槽位画右下角的堆叠数字（>1 才画），item 图标本身不画。", map[string]any{"minimum": 0, "default": 0}),
			"damage":          field("integer", "已消耗的耐久度。与 maxDamage 同时 >0 时在图标上画耐久条。", map[string]any{"default": 0}),
			"maxDamage":       field("integer", "最大耐久度。与 damage 同时 >0 时在图标上画耐久条。", map[string]any{"default": 0}),
			"customModelData": field("integer", "原版 custom_model_data，驱动模型的 select / range_dispatch 覆盖。", nil),
			"customData":      field("integer", "ItemsAdder 的 custom_model_data 数值：与 id 的基础材料组合命中 data/contents 的自定义物品，未命中回退原版物品。不能为负。", map[string]any{"minimum": 0}),
			"components":      field("object", "原版组件表，键可带或不带 minecraft: 前缀（minecraft:profile、banner_patterns、dyed_color 等），值就是组件自身的 JSON。", nil),
			"nbt":             field("object", "任意 NBT/JSON。tooltip 的 advanced 模式用它生成 item.nbt_tags 计数行。", nil),
			"name":            field("string", "悬停名，tooltip 的第一行；支持 § 与 & 颜色码。", nil),
			"lore":            field("array", "tooltip 正文，最多 128 行、每行 1024 字符。", map[string]any{"items": map[string]any{"type": "string"}, "maxItems": 128}),
			"enchantments":    field("array", "附魔，写法 \"sharpness 5\" 或 \"minecraft:sharpness V\"；同时让物品图标出现附魔光效（glint）。", map[string]any{"items": map[string]any{"type": "string"}}),
			"texture":         field("string", "仅 minecraft:player_head 生效：头颅皮肤，内联 data:image/png;base64,...（64×64，或 legacy 64×32 会自动镜像左右肢体）或资源树里的贴图 id（带命名空间或 .png 后缀，如 minecraft:textures/entity/player/wide/alex.png）。不能是网络 URL（400 invalid_data），其他物品传了忽略。", nil),
		},
	}
}

func slotSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "一个槽位。容器的 slots / items 与 playerInventory 的 slots / armor 共用。",
		"properties": map[string]any{
			"slot":  field("integer", "菜单槽位下标 0–255。容器的 0..rows*9-1 属于容器区，之后是玩家背包区。", map[string]any{"minimum": 0, "maximum": 255}),
			"item":  schemaRef("Item"),
			"empty": field("boolean", "标记该槽位为空，此时可以省略 item.id。", map[string]any{"default": false}),
		},
	}
}

func containerDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "inventory、item_container、enderchest 共用的容器界面。",
		"properties": map[string]any{
			"title":     field("string", "界面标题，按原版 AbstractContainerScreen 画在左上角，支持 § 与 & 颜色码。", nil),
			"container": field("string", "仅 item_container 必填。未知容器返回 422 unsupported_container。", map[string]any{"enum": []string{"hopper", "furnace", "blast_furnace", "smoker", "crafting_table", "crafting", "anvil", "brewing_stand", "brewing"}}),
			"rows":      field("integer", "行数。inventory 1–6，缺省 3；enderchest 只能 3；其余容器忽略。", map[string]any{"minimum": 1, "maximum": 6, "default": 3}),
			"columns":   field("integer", "列数，只能是 9（缺省 9）。", map[string]any{"enum": []int{9}, "default": 9}),
			"slots":     field("array", "槽位列表。", map[string]any{"items": schemaRef("Slot")}),
			"items":     field("array", "slots 的别名；两者都给时以 slots 为准。", map[string]any{"items": schemaRef("Slot")}),
		},
	}
}

func tooltipDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "原版悬浮提示框。item.id 与 title 至少给一个。",
		"properties": map[string]any{
			"item":     schemaRef("Item"),
			"title":    field("string", "显式标题行；给出后不再按原版顺序生成行。", nil),
			"advanced": field("boolean", "原版 F3+H 高级提示：追加物品 id、组件 dump 与计数，并叠加 tooltip/frame 边框。", map[string]any{"default": false}),
			"icon":     field("boolean", "在左侧绘制 72px 物品图标。", map[string]any{"default": false}),
		},
	}
}

func skinDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "玩家模型的皮肤。",
		"required":    []string{"texture"},
		"properties": map[string]any{
			"texture":    field("string", "必填：皮肤 PNG，内联 data:image/png;base64,...（64×64，或 legacy 64×32）或资源树里的贴图 id（带命名空间或 .png 后缀，如 minecraft:textures/entity/player/wide/alex.png）。不能是网络 URL。", nil),
			"model":      field("string", "模型种类，缺省 classic。", map[string]any{"enum": []string{"classic", "slim"}, "default": "classic"}),
			"slim":       field("boolean", "model 的等价简写：true 等同 model=slim。", map[string]any{"default": false}),
			"cape":       field("string", "斗篷 PNG，64×32 或 22×17。", nil),
			"outerLayer": field("boolean", "第二层（头发 / 外套 / 袖子 / 裤腿）。", map[string]any{"default": false}),
			"textureUrl": field("string", "禁止：任何非空值都返回 400 invalid_data（skin.textureUrl is not accepted），皮肤只能内联或走资源路径。", nil),
		},
	}
}

func playerInventoryDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "原版 InventoryScreen 1:1 布局，默认输出 176×166。",
		"required":    []string{"skin"},
		"properties": map[string]any{
			"title":    field("string", "标题，按原版 titleLabelX=97、行 6 绘制，支持 § 与 & 颜色码。", nil),
			"skin":     schemaRef("SkinData"),
			"slots":    field("array", "背包槽位，菜单槽 0–26 为背包、27–35 为快捷栏。", map[string]any{"items": schemaRef("Slot")}),
			"armor":    field("array", "穿戴槽 0–3 依次是头盔、胸甲、护腿、靴子；缺失装备定义返回 422。", map[string]any{"items": schemaRef("Slot")}),
			"mainHand": describedRef("Item", "主手物品，按 display.thirdperson_righthand 挂在右手。"),
			"offhand":  describedRef("Item", "副手物品，按 display.thirdperson_lefthand 挂在左手。"),
		},
	}
}

func playerListEntrySchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "TAB 列表的一行。name 与 display 至少给一个，texture 必填。",
		"properties": map[string]any{
			"name":    field("string", "玩家名，与 display 至少给一个。", nil),
			"display": field("string", "显示名，优先于 name；支持 § 与 & 颜色码。", nil),
			"texture": field("string", "必填：头像皮肤，规则同 skin.texture（内联 PNG 或资源路径，不能是网络 URL）。", nil),
			"ping":    field("integer", "延迟毫秒。按官方阈值 <150/<300/<600/<1000 选 ping_5..ping_1；<=0 或缺省用 ping_unknown。", map[string]any{"default": 0}),
		},
	}
}

func playerListDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "原版 TAB 玩家列表。",
		"properties": map[string]any{
			"header":  field("string", "页眉，按 \\n 分行并逐行居中。", nil),
			"footer":  field("string", "页脚，规则同 header。", nil),
			"players": field("array", "玩家条目，最多 500 人。", map[string]any{"items": schemaRef("PlayerListEntry"), "maxItems": 500}),
			"columns": field("integer", "列数。0 表示自动：>40 人 3 列、>20 人 2 列、否则 1 列。只接受 0–16；渲染器只画得下 1–8 列，9–16 会因画布宽度超过 MAX_IMAGE_WIDTH 而以 422 失败。", map[string]any{"minimum": 0, "maximum": 16, "default": 0}),
		},
	}
}

func mapMarkerSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "地图上的一个装饰图标。",
		"properties": map[string]any{
			"x":        field("integer", "地图内 x，0–127，缺省 0。", map[string]any{"minimum": 0, "maximum": 127, "default": 0}),
			"z":        field("integer", "地图内 z，0–127，缺省 0。", map[string]any{"minimum": 0, "maximum": 127, "default": 0}),
			"rotation": field("integer", "朝向，0–15，每个单位 22.5°。", map[string]any{"minimum": 0, "maximum": 15, "default": 0}),
			"type":     field("string", "原版装饰名或别名（player、target_point、<颜色>_banner、plains_village…）；未收录的名称返回 422。", nil),
			"name":     field("string", "标记名，会绘制在地图上。", nil),
		},
	}
}

func mapDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "原版地图图像，默认输出 140×140。",
		"properties": map[string]any{
			"pixels":    field("array", "16384 项扁平调色板下标数组，index = z*128+x，0 表示透明，取值 0–255。", map[string]any{"items": map[string]any{"type": "integer", "minimum": 0, "maximum": 255}, "maxItems": 16384}),
			"pixelData": field("string", "像素字节的另一种写法：base64: 前缀（base64 编码）或原始字节字符串，解码后最多 16384 字节，每字节即调色板下标。", nil),
			"markers":   field("array", "装饰图标。", map[string]any{"items": schemaRef("MapMarker")}),
			"locked":    field("boolean", "在左下角绘制 LOCKED（服务辅助信息，非原版元素）。", map[string]any{"default": false}),
			"dimension": field("string", "在左上角绘制维度名（服务辅助信息，非原版元素）。", nil),
		},
	}
}

func achievementDataSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "官方 advancement toast，默认输出 480×96。",
		"required":    []string{"title", "icon"},
		"properties": map[string]any{
			"title":       field("string", "必填，最多 256 字符。完成态为黄色。", nil),
			"description": field("string", "描述行，最多 1024 字符。", nil),
			"icon":        schemaRef("Item"),
			"completed":   field("boolean", "完成态：使用 obtained 边框与黄色标题。", map[string]any{"default": false}),
			"frame":       field("string", "边框种类，缺省 task。", map[string]any{"enum": []string{"task", "goal", "challenge"}, "default": "task"}),
		},
	}
}

func schemaRef(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// describedRef is a schema reference that still carries the field description, which OpenAPI 3.1
// allows next to $ref.
func describedRef(name, description string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name, "description": description}
}

// field builds one property schema: its JSON type, its Chinese description, and the keywords
// (enum, minimum, maximum, default, items, pattern) the contract needs.
func field(kind, description string, extra map[string]any) map[string]any {
	schema := map[string]any{"type": kind, "description": description}
	for key, value := range extra {
		schema[key] = value
	}
	return schema
}

// sourceOperation reports every directory and file a render reads: the prepared Minecraft versions
// with their font assets, the custom content index of the user packs and the resource pack stack.
func sourcesOperation() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List the resource paths this service reads",
			"description": "返回当前读取的各类资源路径：DATA_DIR、resourcePacks（材质包目录、已加载的包文件与解包路径，与 GET /api/v1/resources/packs 相同）、每个已准备版本的 resourceRoot / vanillaRoot / addonRoot / packRoots 与字体文件（default.json、unifont.zip、font-overlay.json）是否存在，以及 data/contents 的目录、items_ids_cache.yml、解析到的 pack 与条目计数。packRoots 是实时的材质包栈，与版本树无关。目录不存在时状态为 ready、条目为 0，不会报错。",
			"responses":   map[string]any{"200": map[string]any{"description": "Resource paths and their state"}},
		},
	}
}

// resourcePacksOperation documents the user resource pack stack: the files that are loaded, the order
// that decides which one overrides which, and the request that replaces that order.
func resourcePacksOperation() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"summary":     "List the resource packs and their priority",
			"description": "返回 RESOURCE_PACK_ROOT（默认 <DATA_DIR>/resourcepacks）中的压缩包与加载顺序。packs 的数组顺序就是加载顺序：第 1 个最先加载、被后面的覆盖，所以最后一项优先级最高（vanilla → addon → 包 1 → 包 2 …）。每个包在包根之上再叠加 pack.mcmeta 中覆盖当前渲染版本格式（75）的 overlays（见 packs[].overlays），因此包内版本专属的物品定义与贴图会生效；一个包的 overlay 不会越过下一个包。默认顺序在首次发现时按文件修改时间升序、再按文件名升序确定，并持久化到 <DATA_DIR>/states/resource-packs.json；之后新放入的包一律追加到末尾，不会改变已有包的覆盖关系。损坏、元数据无法解析或格式不兼容的包以 status=failed 列出且不参与渲染；压缩包声明的大小/CRC 与数据不符（例如 ItemsAdder 生成包把两者写成占位值）不影响加载，CRC 声明为非 0 时仍校验。目录被监听（RESOURCE_PACK_WATCH_INTERVAL，默认 1s），放入、替换或删除压缩包都会自动重载，无需重启。",
			"responses":   map[string]any{"200": map[string]any{"description": "The resource pack stack"}},
		},
		"put": map[string]any{
			"summary":     "Set the resource pack priority",
			"description": "用 body.order 全量替换加载顺序，数组顺序即新的优先级（越靠后越优先），立即对下一次渲染生效并持久化。order 必须恰好包含 GET 返回的 packs 中每个 file 一次；未知、重复、缺失或空文件名返回 400 invalid_pack_order，原顺序保持不变。",
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": schemaRef("ResourcePackOrder"),
						"examples": map[string]any{
							"load-order": map[string]any{"summary": "base.zip 先加载，overlay.zip 覆盖它", "value": map[string]any{"order": []string{"base.zip", "overlay.zip"}}},
						},
					},
				},
			},
			"responses": map[string]any{"200": map[string]any{"description": "The reordered stack"}, "400": map[string]any{"description": "Invalid pack order"}},
		},
	}
}

func reloadOperation() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Rebuild the custom content index",
			"description": "重新扫描 CUSTOM_CONTENTS_DIR 与 items_ids_cache.yml，返回与 GET /api/v1/resources/sources 相同的 customContents 段。用户手动维护 pack 或索引后调用即可生效，无需重启；扫描期间已缓存的模型与贴图会被丢弃。",
			"responses":   map[string]any{"200": map[string]any{"description": "The rebuilt custom content index"}},
		},
	}
}

// renderOperation documents POST /api/v1/render together with one prefilled payload per supported
// type; Scalar offers them through its example switcher.
func renderOperation() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Render a Minecraft 1.21.11 asset",
			"description": "requestBody 中的每个示例都是可直接发送的完整报文，覆盖 item、tooltip、inventory、item_container、enderchest、playerInventory、skin、playerlist、map、achievement，以及 player_head 的空头颅皮肤用法。示例统一使用 output=base64，响应会返回 PNG 的 base64 与宽高，便于在文档内查看结果；需要二进制图片时把 output 改成 png。渲染前必须先 PUT /api/v1/resources/1.21.11。物品入参可带 customData 指向 data/contents 的自定义物品；材质包栈（会覆盖 vanilla 与 addon）见 GET /api/v1/resources/packs，读取了哪些资源见 GET /api/v1/resources/sources。",
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema":   map[string]any{"$ref": "#/components/schemas/RenderRequest"},
						"examples": renderExamplesDocument(),
					},
				},
			},
			"responses": map[string]any{"200": map[string]any{"description": "PNG or base64 PNG"}, "400": map[string]any{"description": "Invalid request"}, "409": map[string]any{"description": "Resources are not ready"}, "422": map[string]any{"description": "Render failure"}, "503": map[string]any{"description": "Real renderer unavailable"}},
		},
	}
}

// renderExample is one prefilled request payload for POST /api/v1/render.
type renderExample struct {
	kind        string
	summary     string
	description string
	payload     string
}

// renderExamples returns one runnable payload per supported render type. The
// order below is the order Scalar presents them in its example switcher.
func renderExamples() []renderExample {
	return []renderExample{
		{
			kind:        model.TypeItem,
			summary:     "item · 物品图标",
			description: "1.21.11 item definition：模型继承、generated/handheld、纹理层与 glint 都由真实资源决定；带 elements 的模型与 special 模型走同一条 3D quad 管线（gui display 变换 + 正交相机 + 原版方块面明暗：上 1.0、南北 0.8、东西 0.6、下 0.5），方块图标是逐面贴图的标准等轴立方体。minecraft:special 模型按客户端代码直译的几何渲染（旗帜 / 床 / 潜影盒 / 头颅与玩家头 / 盾牌 / 潮涌核心 / 饰纹陶罐 / 铜傀儡雕像 / 三叉戟），组件驱动图案、染色、陶片与内联皮肤；潜影盒按 ShulkerBoxModel 的 lid + base 两个盒子渲染成 3D 方块，贴图取模型定义里已染色的 entity/shulker/shulker（16 色）而不再是平面贴图；箱子族仍为平面贴图。模型不可用时画原版紫黑缺失模型，不返回 422。物品可带 customData（ItemsAdder 的 custom_model_data 数值），与 id 的基础材料组合命中 data/contents 里的自定义物品；命中后按其贴图与模型几何绘制，未命中回退原版并记一条警告。minecraft:player_head 还可以用顶层 texture 直接传 64×64 皮肤，见最后一个示例。options.scale 在这里演示固定倍率放大。",
			payload: `{
  "version": "1.21.11",
  "type": "item",
  "output": "base64",
  "data": {
    "id": "minecraft:diamond_sword",
    "count": 1
  },
  "options": {
    "scale": 2
  }
}`,
		},
		{
			kind:        model.TypeTooltip,
			summary:     "tooltip · 悬浮提示",
			description: "按原版 1.21.11 TooltipRenderUtil 渲染：文本块四周各外扩 12px，用 gui/sprites/tooltip/background 九宫格铺底（advanced 时叠加 tooltip/frame），行高 10px、标题后额外 2px，文本始终带阴影。文本支持 § 与 & 两种颜色/修饰符前缀（0-9a-f、l/o/n/m/k/r；§k 为确定性渲染，不做随机化），另支持任意 RGB 的 §x§R§R§G§G§B§B 写法（六组前缀+十六进制数字，缺一组则整段按字面保留），只有后跟合法码字符时才算格式码。未显式给出 title 时按原版顺序生成行：悬停名、advanced 物品 id、lore、附魔、属性修饰符、单值组件、advanced 组件与 NBT 计数；lore 只来自 item.lore（或 components[\"minecraft:lore\"]），位置固定在悬停名之后，默认 §5 紫色斜体，显式格式码优先。icon 默认关闭，设为 true 时在左侧绘制物品图标。",
			payload: `{
  "version": "1.21.11",
  "type": "tooltip",
  "output": "base64",
  "data": {
    "item": {
      "id": "minecraft:diamond_sword",
      "count": 1,
      "lore": ["§7深海锻造的剑", "§5字形来自 unifont.zip 的 16x16 点阵"],
      "enchantments": ["minecraft:sharpness 5"],
      "components": {
        "minecraft:unbreakable": {},
        "minecraft:enchantments": {"levels": {"minecraft:sharpness": 5}},
        "minecraft:attribute_modifiers": {
          "modifiers": [
            {"type": "minecraft:attack_damage", "amount": 7, "operation": "add_value", "slot": "mainhand"}
          ]
        }
      },
      "nbt": {"CustomTag": 1}
    },
    "advanced": true
  },
  "options": {
    "width": 420,
    "scale": {"ratio": 2}
  }
}`,
		},
		{
			kind:        model.TypeInventory,
			summary:     "inventory · 容器界面",
			description: "官方 generic_54 容器 GUI，rows 1-6、columns 固定 9。标题按原版 AbstractContainerScreen 坐标绘制（左上角 +8,+6），可用 § 或 & 颜色码，居中按去码后的宽度；槽位 0 到 rows*9-1 属于容器区，之后是玩家背包区（本例从 54 开始）；堆叠数量按原版 renderItemDecorations 画在槽位右下角并带阴影。资源缺失或模型不可用的槽位画原版紫黑缺失模型，整图仍然产出。",
			payload: `{
  "version": "1.21.11",
  "type": "inventory",
  "output": "base64",
  "data": {
    "title": "Large Chest",
    "rows": 6,
    "columns": 9,
    "slots": [
      {"slot": 0, "item": {"id": "minecraft:diamond_sword", "count": 1}},
      {"slot": 8, "item": {"id": "minecraft:golden_apple", "count": 3}},
      {"slot": 13, "item": {"id": "minecraft:diamond", "count": 64}},
      {"slot": 53, "item": {"id": "minecraft:elytra", "count": 1}},
      {"slot": 54, "item": {"id": "minecraft:cooked_beef", "count": 32}},
      {"slot": 63, "item": {"id": "minecraft:redstone", "count": 64}}
    ]
  }
}`,
		},
		{
			kind:        model.TypeItemContainer,
			summary:     "item_container · 特殊容器",
			description: "container 支持 hopper、furnace/blast_furnace/smoker、crafting_table、anvil、brewing_stand，槽位坐标取自各自的原版菜单（熔炉 0 输入 (56,17)、1 燃料 (56,53)、2 输出 (116,35)，3 起为玩家区）；漏斗界面按原版为 176×133，其余为 176×166。未知容器返回 unsupported_container。",
			payload: `{
  "version": "1.21.11",
  "type": "item_container",
  "output": "base64",
  "data": {
    "container": "furnace",
    "title": "Furnace",
    "slots": [
      {"slot": 0, "item": {"id": "minecraft:raw_iron", "count": 16}},
      {"slot": 1, "item": {"id": "minecraft:coal", "count": 8}},
      {"slot": 2, "item": {"id": "minecraft:iron_ingot", "count": 4}},
      {"slot": 3, "item": {"id": "minecraft:stone", "count": 64}}
    ]
  }
}`,
		},
		{
			kind:        model.TypeEnderchest,
			summary:     "enderchest · 末影箱",
			description: "固定 3 行 9 列。原版末影箱就是普通三行容器：同一个 generic_54 贴图、同一套配色与槽位，没有任何紫色叠加或额外图标。",
			payload: `{
  "version": "1.21.11",
  "type": "enderchest",
  "output": "base64",
  "data": {
    "title": "Ender Chest",
    "rows": 3,
    "columns": 9,
    "slots": [
      {"slot": 0, "item": {"id": "minecraft:ender_pearl", "count": 16}},
      {"slot": 7, "item": {"id": "minecraft:diamond", "count": 64}},
      {"slot": 26, "item": {"id": "minecraft:shulker_box", "count": 1}}
    ]
  }
}`,
		},
		{
			kind:        model.TypePlayerInventory,
			summary:     "playerInventory · 玩家背包",
			description: "原版 InventoryScreen 1:1 布局：默认输出 176×166，container/inventory 贴图不缩放；标题按原版 titleLabelX=97、行 6 绘制（支持 § 或 & 颜色码）；3D 玩家按原版预览视口渲染（x 26→75、y 8→78，30 像素/方块，脚底对齐视口中心 + (bbHeight/2+0.0625)×30，3/4 正交视角），槽位沿用原版 InventoryMenu 坐标（背包第 0 格 8/84、快捷栏 8/142、装备 8/8+18i、副手 77/62）。armor 按顺序绘制头盔/胸甲/护腿/靴子的原版装备层（含染色与贴图分层），mainHand/offhand 用 display.thirdperson_righthand / thirdperson_lefthand 的 T·R·S 变换挂在对应手上，并乘上原版实体模型空间到场景空间的转换（y 向下/颈原点/朝 -z → y 向上/脚底原点/朝 +z），因此物品落在手心高度、躯干前方并参与同一深度排序；special 模型（旗帜/盾牌/潜影盒/头颅/潮涌核心/陶罐/铜傀儡雕像/三叉戟）在手持时也使用其 3D 几何而不是平面贴图。显式给出 width 与 height 时整图最近邻缩放，只给一个时按 176×166 的比例换算。",
			payload: `{
  "version": "1.21.11",
  "type": "playerInventory",
  "output": "base64",
  "data": {
    "title": "Inventory",
    "skin": {
      "texture": "{{skinTexture}}",
      "model": "classic",
      "outerLayer": true
    },
    "slots": [
      {"slot": 0, "item": {"id": "minecraft:diamond_sword", "count": 1}},
      {"slot": 9, "item": {"id": "minecraft:diamond", "count": 64}},
      {"slot": 18, "item": {"id": "minecraft:golden_apple", "count": 3}},
      {"slot": 27, "item": {"id": "minecraft:elytra", "count": 1}}
    ],
    "armor": [
      {"slot": 0, "item": {"id": "minecraft:diamond_helmet"}},
      {"slot": 1, "item": {"id": "minecraft:diamond_chestplate"}},
      {"slot": 2, "item": {"id": "minecraft:diamond_leggings"}},
      {"slot": 3, "item": {"id": "minecraft:diamond_boots"}}
    ],
    "mainHand": {"id": "minecraft:diamond_sword"},
    "offhand": {"id": "minecraft:diamond"}
  }
}`,
		},
		{
			kind:        model.TypeSkin,
			summary:     "skin · 皮肤",
			description: "只能使用经过校验的内联 64x64 PNG（或已登记资源路径），不支持网络 URL；model 为 classic 或 slim，outerLayer 控制头发/外套/袖子/裤腿第二层；cape 使用原版斗篷贴图布局，legacy 64x32 皮肤会自动补齐左侧肢体。",
			payload: `{
  "version": "1.21.11",
  "type": "skin",
  "output": "base64",
  "data": {
    "texture": "{{skinTexture}}",
    "model": "slim",
    "outerLayer": true
  }
}`,
		},
		{
			kind:        model.TypePlayerList,
			summary:     "playerlist · 玩家列表",
			description: "原版 TAB 列表布局：9px 行距（2 倍渲染，字体同步放大 2 倍）、8x8 头像（皮肤 (8,8) 与外层 (40,8)）、右侧 ping 图标与官方阈值（<150/<300/<600/<1000 对应 ping_5..ping_1，ping<=0 为 ping_unknown），每列最多 20 行、多列从左到右、header/footer 居中。玩家名与 header/footer 都支持 § 或 & 颜色码，列宽与居中按去码后的宽度计算。columns 缺省为 0（自动：>40 人 3 列、>20 人 2 列、否则 1 列）；渲染器只画得下 1–8 列，9–16 会因画布宽度超过 MAX_IMAGE_WIDTH 以 422 失败。",
			payload: `{
  "version": "1.21.11",
  "type": "playerlist",
  "output": "base64",
  "data": {
    "header": "Survival Server",
    "footer": "play.example.com",
    "columns": 2,
    "players": [
      {"name": "Steve", "display": "Steve", "texture": "{{skinTexture}}", "ping": 42},
      {"name": "Alex", "display": "Alex", "texture": "{{skinTexture}}", "ping": 88},
      {"name": "Herobrine", "display": "Herobrine", "texture": "{{skinTexture}}", "ping": 5}
    ]
  }
}`,
		},
		{
			kind:        model.TypeMap,
			summary:     "map · 地图",
			description: "原版地图图像：minecraft:map/map_background 羊皮纸背景 + 内缩的完整 128x128 像素网格（默认输出 140x140，内容与像素 1:1）+ minecraft:map/decorations 原版装饰图标（按 rotation*22.5° 旋转）。pixels 为 16384 项扁平数组（index = z*128+x，0 透明），也可用解码后不超过 16384 字节的 base64: pixelData；未知装饰类型返回 422。",
			payload: `{
  "version": "1.21.11",
  "type": "map",
  "output": "base64",
  "data": {
    "pixels": {{mapPixels}},
    "markers": [
      {"x": 32, "z": 16, "rotation": 4, "type": "player", "name": "Spawn"},
      {"x": 96, "z": 64, "rotation": 12, "type": "red_banner", "name": "Base"},
      {"x": 64, "z": 100, "rotation": 0, "type": "target_point", "name": "Goal"}
    ],
    "scale": 2,
    "dimension": "minecraft:overworld",
    "locked": true
  }
}`,
		},
		{
			kind:        model.TypeAchievement,
			summary:     "achievement · 成就提示",
			description: "官方 advancement toast：160×32 的 toast 内容按默认 480×96 放大三倍；frame 为 task/goal/challenge，completed 决定 obtained/unobtained 边框，icon 为物品图标；标题与描述按原版 AdvancementToast 绘制在 x=30 的第 7、18 行，支持 § 或 & 颜色码。",
			payload: `{
  "version": "1.21.11",
  "type": "achievement",
  "output": "base64",
  "data": {
    "title": "Diamonds!",
    "description": "Acquire a diamond",
    "icon": {"id": "minecraft:diamond", "count": 1},
    "frame": "challenge",
    "completed": true
  }
}`,
		},
		{
			kind:        "player_head",
			summary:     "player_head · 玩家头颅",
			description: "minecraft:player_head 的皮肤来源按优先级：item.texture（本例）→ components[\"minecraft:profile\"] 里内联的皮肤 → 客户端默认皮肤 entity/player/wide/steve。item.texture 接受内联 data:image/png;base64,...（64×64，或 legacy 64×32 自动镜像左右肢体）或已准备资源树里的贴图路径；不联网解析 username / uuid / 远端 hash，网络 URL 直接 400。贴图损坏时只打一行 stderr 并回退到 profile，仍返回 200。例子里用的是服务启动时生成的 64×64 占位皮肤；同一条数据在手持、容器槽位与 tooltip 图标里同样生效。",
			payload: `{
  "version": "1.21.11",
  "type": "item",
  "output": "base64",
  "data": {
    "id": "minecraft:player_head",
    "texture": "{{skinTexture}}"
  }
}`,
		},
	}
}

// renderExamplesDocument renders the catalogue into the OpenAPI examples object.
func renderExamplesDocument() orderedObject {
	replacements := map[string]string{
		skinTexturePlaceholder: sampleSkinDataURI(),
		mapPixelsPlaceholder:   marshalJSON(sampleMapPixels()),
	}
	catalog := renderExamples()
	examples := make(orderedObject, 0, len(catalog))
	for _, example := range catalog {
		payload := example.payload
		for placeholder, value := range replacements {
			payload = strings.ReplaceAll(payload, placeholder, value)
		}
		examples = append(examples, orderedEntry{key: example.kind, value: map[string]any{
			"summary":     example.summary,
			"description": example.description,
			"value":       json.RawMessage(payload),
		}})
	}
	return examples
}

// sampleSkinDataURI builds a deterministic 64x64 placeholder skin so the skin,
// playerInventory, and playerlist examples run without external files.
func sampleSkinDataURI() string {
	canvas := image.NewRGBA(image.Rect(0, 0, 64, 64))
	paint := func(rect image.Rectangle, value color.RGBA) {
		draw.Draw(canvas, rect, &image.Uniform{C: value}, image.Point{}, draw.Src)
	}
	skin := color.RGBA{R: 0xD8, G: 0xA8, B: 0x7F, A: 0xFF}
	shirt := color.RGBA{R: 0x3A, G: 0x5F, B: 0xA8, A: 0xFF}
	trousers := color.RGBA{R: 0x2F, G: 0x3A, B: 0x6E, A: 0xFF}
	paint(canvas.Bounds(), skin)
	paint(image.Rect(20, 20, 28, 32), shirt)
	paint(image.Rect(32, 20, 40, 32), shirt)
	paint(image.Rect(44, 20, 48, 32), shirt)
	paint(image.Rect(36, 52, 40, 64), shirt)
	paint(image.Rect(4, 20, 8, 32), trousers)
	paint(image.Rect(20, 52, 24, 64), trousers)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// sampleMapPixels builds a deterministic complete 128x128 map for the
// OpenAPI/Scalar example. Keeping every cell populated makes the example
// visibly demonstrate the complete map rather than only its top-left rows.
func sampleMapPixels() []int {
	pixels := make([]int, 128*128)
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			value := 1 + (x/16+y/16)%6
			if (x-64)*(x-64)+(y-64)*(y-64) < 28*28 {
				value = 7 + (x+y)%5
			}
			pixels[y*128+x] = value
		}
	}
	return pixels
}

func marshalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(encoded)
}

// orderedObject encodes JSON members in insertion order. encoding/json sorts Go
// map keys, which would scatter the render examples; the order in this file is
// the order Scalar presents them in its example switcher.
type orderedObject []orderedEntry

type orderedEntry struct {
	key   string
	value any
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, entry := range o {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(entry.key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(entry.value)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}
