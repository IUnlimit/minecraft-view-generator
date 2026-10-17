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
			"/healthz":                    map[string]any{"get": map[string]any{"summary": "Health and renderer capability check", "responses": map[string]any{"200": map[string]any{"description": "Service status and backend capability"}}}},
			"/api/v1/versions":            map[string]any{"get": map[string]any{"summary": "List local resource contexts", "responses": map[string]any{"200": map[string]any{"description": "Version states"}}}},
			"/api/v1/resources/{version}": map[string]any{"parameters": []any{map[string]any{"name": "version", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}}, "put": map[string]any{"summary": "Prepare resources", "responses": map[string]any{"202": map[string]any{"description": "Preparation started"}, "200": map[string]any{"description": "Already ready"}}}, "get": map[string]any{"summary": "Read preparation state", "responses": map[string]any{"200": map[string]any{"description": "Resource state"}, "404": map[string]any{"description": "Unknown version"}}}},
			"/api/v1/render":              renderOperation(),
		},
		"components": map[string]any{"schemas": map[string]any{"RenderRequest": map[string]any{"type": "object", "required": []string{"version", "type", "output", "data"}, "properties": map[string]any{"version": map[string]any{"type": "string", "const": model.SupportedVersion}, "type": map[string]any{"type": "string", "enum": model.SupportedTypes()}, "output": map[string]any{"type": "string", "enum": []string{model.OutputPNG, model.OutputBase64}}, "data": map[string]any{"type": "object"}, "options": map[string]any{"type": "object", "properties": map[string]any{"projection": map[string]any{"type": "string", "enum": []string{"gui", "orthographic"}}, "width": map[string]any{"type": "integer", "minimum": 16}, "height": map[string]any{"type": "integer", "minimum": 16}, "scale": map[string]any{"type": "integer", "minimum": 0, "maximum": 4}, "frame": map[string]any{"type": "integer", "minimum": 0}, "transparent": map[string]any{"type": "boolean"}}}}}, "Error": map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{"error": map[string]any{"type": "object", "required": []string{"code", "message"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}}}}}}},
	}
}

// renderOperation documents POST /api/v1/render together with one prefilled
// payload per supported type; Scalar offers them through its example switcher.
func renderOperation() map[string]any {
	return map[string]any{
		"post": map[string]any{
			"summary":     "Render a Minecraft 1.21.11 asset",
			"description": "requestBody 中的每个示例都是可直接发送的完整报文，覆盖 item、tooltip、inventory、item_container、enderchest、playerInventory、skin、playerlist、map、achievement。示例统一使用 output=base64，响应会返回 PNG 的 base64 与宽高，便于在文档内查看结果；需要二进制图片时把 output 改成 png。渲染前必须先 PUT /api/v1/resources/1.21.11。",
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
			description: "1.21.11 item definition：模型继承、generated/handheld、纹理层与 glint 都由真实资源决定。minecraft:special 模型按客户端代码直译的几何渲染（旗帜 / 床 / 头颅与玩家头 / 盾牌 / 潮涌核心 / 饰纹陶罐 / 铜傀儡雕像 / 三叉戟），组件驱动图案、染色、陶片与内联皮肤；箱子族仍为平面贴图。模型不可用时画原版紫黑缺失模型，不返回 422。",
			payload: `{
  "version": "1.21.11",
  "type": "item",
  "output": "base64",
  "data": {
    "id": "minecraft:diamond_sword",
    "count": 1
  }
}`,
		},
		{
			kind:        model.TypeTooltip,
			summary:     "tooltip · 悬浮提示",
			description: "按原版 1.21.11 TooltipRenderUtil 渲染：文本块四周各外扩 12px，用 gui/sprites/tooltip/background 九宫格铺底（advanced 时叠加 tooltip/frame），行高 10px、标题后额外 2px，文本始终带阴影。文本支持 § 颜色与修饰符（§0-9a-f、§l/o/n/m/k/r；§k 为确定性渲染，不做随机化）。未显式给出 title 时按原版顺序生成行：悬停名、advanced 物品 id、lore、附魔、属性修饰符、单值组件、advanced 组件与 NBT 计数。icon 默认关闭，设为 true 时在左侧绘制物品图标。",
			payload: `{
  "version": "1.21.11",
  "type": "tooltip",
  "output": "base64",
  "data": {
    "item": {
      "id": "minecraft:diamond_sword",
      "count": 1,
      "lore": ["Forged in the deep"],
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
    "lines": ["§7A grey lore style line"],
    "advanced": true
  },
  "options": {
    "width": 420
  }
}`,
		},
		{
			kind:        model.TypeInventory,
			summary:     "inventory · 容器界面",
			description: "官方 generic_54 容器 GUI，rows 1-6、columns 固定 9。标题按原版 AbstractContainerScreen 坐标绘制（左上角 +8,+6）；槽位 0 到 rows*9-1 属于容器区，之后是玩家背包区（本例从 54 开始）；堆叠数量按原版 renderItemDecorations 画在槽位右下角并带阴影。资源缺失或模型不可用的槽位画原版紫黑缺失模型，整图仍然产出。",
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
			description: "原版 InventoryScreen 1:1 布局：默认输出 176×166，container/inventory 贴图不缩放；标题按原版 titleLabelX=97、行 6 绘制；3D 玩家按原版预览视口渲染（x 26→75、y 8→78，30 像素/方块，脚底对齐视口中心 + (bbHeight/2+0.0625)×30，3/4 正交视角），槽位沿用原版 InventoryMenu 坐标（背包第 0 格 8/84、快捷栏 8/142、装备 8/8+18i、副手 77/62）。armor 按顺序绘制头盔/胸甲/护腿/靴子的原版装备层（含染色与贴图分层），mainHand/offhand 用 display.thirdperson_righthand / thirdperson_lefthand 的 T·R·S 变换挂在对应手上，并乘上原版实体模型空间到场景空间的转换（y 向下/颈原点/朝 -z → y 向上/脚底原点/朝 +z），因此物品落在手心高度、躯干前方并参与同一深度排序；special 模型（旗帜/盾牌/头颅/潮涌核心/陶罐/铜傀儡雕像/三叉戟）在手持时也使用其 3D 几何而不是平面贴图。显式 width/height 时整图最近邻缩放。",
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
			description: "原版 TAB 列表布局：9px 行距（2 倍渲染，字体同步放大 2 倍）、8x8 头像（皮肤 (8,8) 与外层 (40,8)）、右侧 ping 图标与官方阈值（<150/<300/<600/<1000 对应 ping_5..ping_1，ping<=0 为 ping_unknown），每列最多 20 行、多列从左到右、header/footer 居中。gameMode 仍接受校验但不会绘制，以保持原版行布局。",
			payload: `{
  "version": "1.21.11",
  "type": "playerlist",
  "output": "base64",
  "data": {
    "header": "Survival Server",
    "footer": "play.example.com",
    "columns": 2,
    "players": [
      {"name": "Steve", "display": "Steve", "texture": "{{skinTexture}}", "ping": 42, "gameMode": "survival"},
      {"name": "Alex", "display": "Alex", "texture": "{{skinTexture}}", "ping": 88, "gameMode": "creative"},
      {"name": "Herobrine", "display": "Herobrine", "texture": "{{skinTexture}}", "ping": 5, "gameMode": "adventure"}
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
			description: "官方 advancement toast：160×32 的 toast 内容按默认 480×96 放大三倍；frame 为 task/goal/challenge，completed 决定 obtained/unobtained 边框，icon 为物品图标；标题与描述按原版 AdvancementToast 绘制在 x=30 的第 7、18 行。",
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
