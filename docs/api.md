# Minecraft View Generator API

服务固定使用 Minecraft `1.21.11`。默认后端是 Java 21 无头 CPU renderer，读取 Mojang client JAR 与资源包栈；未准备资源时不会返回伪造图片。

## 启动与接口文档

在仓库根目录执行 `./run.sh` 即可一键运行。脚本会在 `dist/minecraft-view-generator2` 不存在时自动执行构建，并默认监听 `0.0.0.0:4666`；也可以用 `BUILD=1 ./run.sh` 强制重新构建。脚本会把数据目录和 Java renderer 路径固定为仓库内的绝对路径，因此可以从任意当前目录调用。

交互式 Scalar API Reference 位于 `http://127.0.0.1:4666/docs`，`/scalar` 是同一个页面的别名；原始 OpenAPI 文档位于 `http://127.0.0.1:4666/api/v1/openapi.json`。Scalar 页面通过 CDN 加载前端，浏览器访问文档页时需要能够访问 jsDelivr。

`POST /api/v1/render` 的 requestBody 内置了十一种可直接发送的报文（Scalar 示例切换器：十种 type 的 `item`、`tooltip`、`inventory`、`item_container`、`enderchest`、`playerInventory`、`skin`、`playerlist`、`map`、`achievement`，外加一个用 `item.texture` 传皮肤的 `player_head` 示例），统一使用 `output=base64`。`data` 的每个结构都在 `components.schemas` 里，逐字段说明见「请求参数总表」。示例中的皮肤是服务启动时生成的 64×64 占位贴图，因此可以直接执行；渲染前仍需先准备资源。

OpenAPI 文档随二进制内嵌生成：调整接口后执行 `make build`（或让 `./run.sh` 自动构建）并重启服务，即完成文档重新生成，随后核对 `/docs` 与 `/api/v1/openapi.json`。

## 资源准备

```sh
curl -X PUT http://localhost:4666/api/v1/resources/1.21.11
curl http://localhost:4666/api/v1/resources/1.21.11
curl http://localhost:4666/api/v1/versions
```

资源层级为 vanilla → addon → 用户材质包（`data/resourcepacks/`，按加载顺序，越靠后越优先），后者覆盖前者。材质包的目录、顺序与接口见下一节。

## 材质包（resource packs）

把材质包压缩包放进 `data/resourcepacks/`（启动时自动创建，目录可用 `RESOURCE_PACK_ROOT` 改），服务启动时自动解压并作为资源栈的最后一层参与渲染：贴图、模型、`items/` 定义、语言文件与字体（`assets/minecraft/font/…`）都从同一个优先级顺序里取，越靠后的包越优先（模型 id 的字面查找与字体 provider 的栈合并见下面两条）。

```sh
cp MyPack.zip data/resourcepacks/                # 放包；服务运行中也会自动重载（默认 1s 轮询）
curl http://localhost:4666/api/v1/resources/packs
curl -X PUT http://localhost:4666/api/v1/resources/packs \
  -H 'Content-Type: application/json' \
  --data '{"order":["Base.zip","MyPack.zip"]}'
```

`GET /api/v1/resources/packs` 返回读取的目录、解包缓存与按**加载顺序**排列的包：

```json
{
  "directory": "/app/data/resourcepacks",
  "cacheRoot": "/app/data/resources/.packs",
  "status": "ready",
  "packs": [
    {"file": "Base.zip", "priority": 1, "addedAt": "2026-01-01T00:00:00Z",
     "root": "/app/data/resources/.packs/pack-1a2b3c4d5e6f7788",
     "files": 312, "packFormat": 46, "status": "loaded"},
    {"file": "MyPack.zip", "priority": 2, "addedAt": "2026-01-02T00:00:00Z",
     "root": "/app/data/resources/.packs/pack-8899aabbccddeeff",
     "overlays": ["ia_overlay_1_21_6_plus", "ia_overlay_modern_atlas"],
     "files": 6773, "packFormat": 75, "repairedImages": 2927, "status": "loaded"}
  ],
  "updatedAt": "2026-01-02T00:00:01Z"
}
```

- `priority` 就是加载顺序，从 1 开始，**数值越大越优先**：`packs` 数组第 1 项最先加载、被后面的覆盖，最后一项覆盖所有人。
- `overlays` 是该包在 1.21.11（资源包格式 **75**）下生效的 overlay 目录，按叠加顺序排列：它们依次覆盖包根，所以包内版本专属的 `items/` 定义与贴图会生效。其它版本的 overlay、目录不存在、`directory` 指到包外（绝对路径或含 `..`）的条目一律忽略；一个包的 overlay 不会越过下一个包。
- 任何 `pack.mcmeta` 写法都能读：`pack_format` / `min_format` / `max_format` 可以是数字或 `[major, minor]` 数组，`supported_formats` 可以是 `{min_inclusive, max_inclusive}` 对象或 `[min, max]` 数组（1.21.9+ 的写法），overlays 条目同理。只有**整段**声明的格式都高于 100（例如 `pack_format: 200`）才判定为面向未来版本并拒绝，`max_format: [1000, 0]` 这种写法照常接受。`packFormat` 报告 `pack_format`，缺省时取声明区间的下界。
- `PUT /api/v1/resources/packs` 的 `order` 必须恰好是 `packs[].file` 的一个排列（每个一次）；数组顺序即新的优先级，立即对下一次渲染生效，并持久化到 `data/states/resource-packs.json`。未知、重复、缺失或空文件名返回 `400 invalid_pack_order`，原顺序保持不变。
- 默认顺序：首次发现时按文件**修改时间**升序、再按**文件名**升序；此后新放入的包一律追加到末尾，不会改变已有包之间的覆盖关系。改名视为「删除 + 新增」。
- 只读取目录下的 `*.zip`（扩展名大小写不敏感）；点开头的条目与其它文件被忽略并记入 `warnings`。目录形式的包（未压缩）不再支持，请打包成 zip。
- 坏包（缺 `pack.mcmeta`、`pack.mcmeta` 无法解析、整段格式声明都高于 100、zip 数据损坏、超 `MAX_ARCHIVE_FILES`/`MAX_ARCHIVE_BYTES`）以 `status: "failed"` + `error` 列出且**不参与渲染**，其余包裹照常；替换或修好后自动恢复，不会让服务起不来。
- 解包只信 deflate 流本身：中央目录声明的大小不可信时按实际字节解出（ItemsAdder 生成包把每条的未压缩大小写成 1337、CRC 写成 0，仍能完整解包）；CRC 声明为非 0 时仍然校验，不符即整包 `failed`。大小上限按**实际写入**的字节数统计，zip 炸弹依旧被挡住。
- 包内 PNG 的校验值错误（chunk CRC、zlib 头与 adler32 都不对，ItemsAdder 生成包的常见形态）在解包后自动修正：deflate 载荷、像素数据与 `iaPR` 这类私有 chunk 原样保留，只重算校验值，原始 zip 一个字节都不动；`packs[].repairedImages` 报告本次扫描改写了多少张。修正幂等，已修好的包重启既不再写盘也不再报告该数字。交错（Adam7）的 PNG 与 inflate 结果和 IHDR 推导长度不符（数据真的坏了）的文件保持原样并记入 `warnings`，这种贴图在渲染时降级为缺失贴图，不会让整次渲染失败。
- zip 解包到 `data/resources/.packs/pack-<sha1>/`，按内容寻址：重启只算一次哈希，不会重复解包；不再被引用的旧缓存目录在启动时清理，整个 `.packs` 目录任何时候都可以删。
- 目录被轮询监听（`RESOURCE_PACK_WATCH_INTERVAL`，默认 `1s`，设为 `0` 关闭）；写入、替换或删除压缩包后最多约 2 个周期自动重载，不需要重启，也不需要重新 `PUT /api/v1/resources/<version>`。
- 材质包是全局的（当前只支持 1.21.11），与版本树无关：调整优先级或重载包都不需要重新准备资源。`RESOURCE_PACK_ROOT` 只表示一个材质包目录；`data/resourcepack`（旧名字）不再被读取，需要手动改名为 `data/resourcepacks`。
- 模型里的贴图引用先按路径读 `assets/<namespace>/textures/<path>.png`，路径不存在时再按包的 `assets/<namespace>/atlases/*.json` 解析**精灵名**：ItemsAdder 生成包把每个精灵改名成 `ia:0`、`ia:1`…（本仓库里的包是 2383 个），只有图集的 `single` 源能说明 `ia:1789` 其实是 `minecraft:item/diamond_sword`，所以这种引用照样能出图。同名的图集文件以优先级最高的包为准（整文件覆盖，不合并），同一文档里靠后的 source 覆盖靠前的；`directory` / `paletted_permutations` / `filter` / `unstitch` 暂不支持，解析不到的名字（包括 atlas 里根本没声明的）照旧降级为缺失贴图，不会让整次渲染失败。
- 模型 id 按**字面**路径解析：`assets/<namespace>/models/<path>.json`，与客户端一致，不补 `item/` 前缀。ItemsAdder 导出的模型就放在 `assets/<包命名空间>/models/<name>.json`（没有 `item/` 子目录），模型的 `overrides` 里直接写 `包命名空间/<name>` 即可；`minecraft:item/diamond` 这种自带 `item/` 的写法照旧。覆盖（`custom_model_data`）指向的模型一个包都没有时按原版语义画**缺失模型**（紫黑）并写一条诊断，不会退回被覆盖的基础物品模型。历史上依赖自动补 `item/` 的包仍能出图：只有字面路径不存在时才回退到 `assets/<namespace>/models/item/<path>.json`，并在 stderr 记一条 `resolved through the legacy item/ prefix`。
- 字体定义 `assets/minecraft/font/default.json` 按资源栈**从高到低拼接** providers，而不是整文件覆盖：包可以只追加自己的 provider（ItemsAdder 生成的 `default.json` 通常是一整串 `bitmap`，没有任何 `include/default`），ASCII 与 CJK 仍由底层副本的 `include/*` 兜底；`reference` 也按同一规则递归，包可以 include 自己的定义再继承底层。单份 JSON 解析失败只丢那一份并写警告，不会让整次渲染失败。
- 已知限制：字体 provider 只支持 `space` / `bitmap` / `unihex` / `reference`（与渲染器一致），只带 `ttf` provider 的字体包会退化成缺字框；上面两条只改变查找与合并的来源，不改 provider 类型与字形度量。

## 统一渲染请求

所有类型都使用：`version`、`type`、`output`、`data`，以及可选的 `options`。每个字段的类型、必填、默认值与上限见下面的**请求参数总表**。原来的 `options.frame` 与 `options.projection` 已删除（Java 渲染器从不读取它们）；由于它们属于顶层 `options` 的字段，继续传会被 `DisallowUnknownFields` 拒绝并返回 `400 invalid_json`。

```sh
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","data":{"id":"minecraft:diamond_sword","count":1}}' \
  -o item.png
```

`output=png` 返回 `image/png`；`output=base64` 返回 `{ "mimeType":"image/png", "encoding":"base64", "data":"...", "width":n, "height":n }`。响应里的 `width`/`height` 就是这张图的最终尺寸。

## 请求参数总表

<!-- params:start -->

下面这几张表就是 `POST /api/v1/render` 的完整契约：字段、类型、必填、默认值与上限。[internal/model/docs_test.go](../internal/model/docs_test.go) 会反射遍历接口结构体，断言每个 json 字段都在本节里以 `` `字段名` `` 的形式出现；漏写一个，`go test ./...` 就会失败。

### 公共字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `version` | string | ✔ | 只能是 `"1.21.11"`，其他版本返回 `400 invalid_data` |
| `type` | string | ✔ | 十种 type 选一，见「每种 type 生成什么图」 |
| `output` | string | ✔ | `png` 返回 `image/png`；`base64` 返回带 `width`/`height` 的 JSON |
| `data` | object | ✔ | 结构由 `type` 决定，见下文各表 |
| `options` | object |  | 画布尺寸、倍率与外观，见下表 |

**三条最容易踩的规则：**

- 顶层字段与 `options` 内的未知字段由 `DisallowUnknownFields` 拒绝 → `400 invalid_json`。所以 `options.frame`、`options.projection` 现在会直接报错。
- `data` 内的未知字段**静默忽略**：既不报错，也不生效。拼错的字段名不会提醒你，只会让效果消失。
- `output=base64` 响应里的 `width`/`height` 是这张图的**最终成品尺寸**，可能不等于 `options.width/height`——`tooltip` 与 `playerlist` 的高度由内容撑开。

### options

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `width` | integer | 各类型原生宽度 | 画布宽度（渲染分辨率），≥16，上限默认 1600（`MAX_IMAGE_WIDTH`） |
| `height` | integer | 按原生比例换算 | 画布高度，≥16，上限默认 1200（`MAX_IMAGE_HEIGHT`） |
| `scale` | number \| object | 不放大 | `2`、`{"ratio":2}`、`{"width":800}`、`{"width":800,"height":600}`；倍率 0–16，允许小数，详见「图片尺寸与放大倍率」 |
| `background` | string | 透明 | `#RRGGBB`（`#` 可省）。**只对 `item` 有效**：先铺满整张画布，图标自身的透明像素会透出这个颜色；格式不对按透明处理 |
| `transparent` | boolean | false | **只对 `item` 有效**：保留图标自身的透明像素并忽略 `background` |

其余九种类型都自绘不透明背景，`background` 与 `transparent` 对它们没有作用。

### item 的物品字段

`item`、`tooltip.item`、`slots[].item`、`armor[]`、`mainHand`/`offhand`、`achievement.icon` 共用同一套字段。

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `id` | string | — | **必填**，如 `minecraft:diamond_sword`；缺失返回 `400 invalid_data` |
| `count` | integer | 0 | 堆叠数量，≥0。只在容器 / 背包槽位画右下角数字（>1 才画），`item` 图标本身不画 |
| `damage` | integer | 0 | 已消耗耐久；与 `maxDamage` 同时 >0 时在图标上画耐久条 |
| `maxDamage` | integer | 0 | 最大耐久；与 `damage` 同时 >0 时在图标上画耐久条 |
| `customData` | integer | — | **原版 `custom_model_data` 数值**，≥0：驱动模型 `overrides` 的 `custom_model_data` predicate 与 item definition 的 `select` / `range_dispatch`，同时是 ItemsAdder 索引键的数值部分（与 `id` 的基础材料组合命中 `data/contents`，见「自定义物品」） |
| `customModelData` | integer | — | 同一数值的旧拼写，等价于 `customData`；两者都传时以 `customData` 为准，新调用方不需要这个字段 |
| `components` | object | — | 原版组件表，键可带或不带 `minecraft:` 前缀 |
| `nbt` | object | — | 任意 NBT/JSON；`tooltip` 的 advanced 模式用它生成 `item.nbt_tags` 计数行 |
| `name` | string | — | 悬停名（tooltip 首行），支持 `§` 与 `&` 颜色码 |
| `lore` | string[] | — | tooltip 正文，≤128 行 × 1024 字符 |
| `enchantments` | string[] | — | `"sharpness 5"` 或 `"minecraft:sharpness V"`；同时触发附魔光效（glint） |
| `texture` | string | — | **仅 `minecraft:player_head`**：头颅皮肤，见「player_head 头颅皮肤」；不能是网络 URL（`400 invalid_data`），其他物品传了忽略 |

### 每种 type 的 data

| type | data 结构 |
| --- | --- |
| `item` | 上一节的物品字段 |
| `tooltip` | `TooltipData` |
| `inventory` / `item_container` / `enderchest` | `ContainerData` |
| `playerInventory` | `PlayerInventoryData` |
| `skin` | `SkinData` |
| `playerlist` | `PlayerListData`（`players[]` 为 `PlayerListEntry`） |
| `map` | `MapData`（`markers[]` 为 `MapMarker`） |
| `achievement` | `AchievementData`（`icon` 为物品字段） |

`Slot`（容器 `slots`/`items` 与 `playerInventory` 的 `slots`/`armor` 共用）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `slot` | integer | 0 | 菜单槽位下标 0–255；容器的 `0..rows*9-1` 是容器区，之后是玩家背包区 |
| `item` | Item | — | 该槽位的物品 |
| `empty` | boolean | false | 标记空槽：此时可以省略 `item.id` |

`ContainerData`（`inventory` / `item_container` / `enderchest`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `title` | string | — | 界面标题，按原版画在左上角，支持 `§` 与 `&` |
| `container` | string | — | **`item_container` 必填**：`hopper`、`furnace`/`blast_furnace`/`smoker`、`crafting_table`（别名 `crafting`）、`anvil`、`brewing_stand`（别名 `brewing`）；未知容器返回 `422 unsupported_container` |
| `rows` | integer | 3 | `inventory` 1–6；`enderchest` 只能 3；其余容器忽略 |
| `columns` | integer | 9 | 只能 9 |
| `slots` | Slot[] | — | 槽位列表 |
| `items` | Slot[] | — | `slots` 的别名；两者都给时 `slots` 优先 |

`TooltipData`（`tooltip`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `item` | Item | — | 物品；`item.id` 与 `title` 至少给一个，否则 `400 invalid_data` |
| `title` | string | — | 显式标题行；给出后不再按原版顺序生成行 |
| `advanced` | boolean | false | 原版 F3+H 高级提示：追加物品 id、组件 dump 与计数，并叠加 `tooltip/frame` 边框 |
| `icon` | boolean | false | 在左侧绘制 72px 物品图标 |

`SkinData`（`skin`，以及 `playerInventory.skin`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `texture` | string | — | **必填**：内联 `data:image/png;base64,...`（64×64，或 legacy 64×32）或已准备资源树里的贴图路径；不能是网络 URL |
| `model` | string | classic | `classic` / `slim` |
| `slim` | boolean | false | 等价写法：`true` 即 `slim` |
| `cape` | string | — | 斗篷 PNG，64×32 或 22×17 |
| `outerLayer` | boolean | false | 第二层（头发 / 外套 / 袖子 / 裤腿） |
| `textureUrl` | string | — | **禁止**：任何非空值都返回 `400 invalid_data`（`skin.textureUrl is not accepted`） |

`PlayerInventoryData`（`playerInventory`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `title` | string | — | 标题，原版 `(97, 6)` |
| `skin` | SkinData | — | **必填**，其中 `texture` 也必填 |
| `slots` | Slot[] | — | 菜单槽 0–26 背包、27–35 快捷栏 |
| `armor` | Slot[] | — | 槽 0–3 依次是头盔、胸甲、护腿、靴子；缺失装备定义返回 `422` |
| `mainHand` | Item | — | 主手物品，挂载时用 `display.thirdperson_righthand` |
| `offhand` | Item | — | 副手物品，挂载时用 `display.thirdperson_lefthand` |

`PlayerListData`（`playerlist`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `header` | string | — | 页眉，按 `\n` 分行并逐行居中 |
| `footer` | string | — | 页脚，规则同 `header` |
| `players` | PlayerListEntry[] | — | 玩家条目，≤500 人 |
| `columns` | integer | 0 | 0 = 自动：>40 人 3 列、>20 人 2 列、否则 1 列。只接受 0–16（17 及以上 `400 invalid_data`）；渲染器只画得下 1–8 列，9–16 会因画布宽度超过 `MAX_IMAGE_WIDTH` 而以 `422` 失败 |

`PlayerListEntry`

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `name` | string | — | 玩家名，与 `display` 至少给一个 |
| `display` | string | — | 显示名，优先于 `name`；支持 `§` 与 `&` |
| `texture` | string | — | **必填**：头像皮肤，规则同 `SkinData.texture`，不能是网络 URL |
| `ping` | integer | 0 | 延迟毫秒，按官方阈值 `<150`/`<300`/`<600`/`<1000` 选 `ping_5..ping_1`；`<=0` 用 `ping_unknown` |

`MapData`（`map`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `pixels` | integer[] | — | 16384 项扁平数组，`index = z*128+x`，0 表示透明，取值 0–255 |
| `pixelData` | string | — | `base64:` 前缀（base64 编码）或原始字节字符串，解码后 ≤16384 字节 |
| `markers` | MapMarker[] | — | 装饰图标 |
| `locked` | boolean | false | 在左下角绘制 `LOCKED`（服务辅助信息，非原版元素） |
| `dimension` | string | — | 在左上角绘制维度名（服务辅助信息，非原版元素） |

`MapMarker`

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `x` | integer | 0 | 地图内 x，0–127 |
| `z` | integer | 0 | 地图内 z，0–127 |
| `rotation` | integer | 0 | 0–15，每个单位 22.5° |
| `type` | string | — | 原版装饰名或别名；未收录的名称返回 `422`，清单见「map 示例」 |
| `name` | string | — | 标记名，会绘制在地图上 |

`AchievementData`（`achievement`）

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `title` | string | — | **必填**，≤256 字符；完成态为黄色 |
| `description` | string | — | 描述行，≤1024 字符 |
| `icon` | Item | — | **必填**（`icon.id` 必填） |
| `completed` | boolean | false | 完成态：obtained 边框 + 黄色标题 |
| `frame` | string | task | `task` / `goal` / `challenge` |

### 每种 type 生成什么图

| type | 原生画布 | 内容 | 背景 | `width`/`height` 的作用 |
| --- | --- | --- | --- | --- |
| `item` | 128×128 正方形 | 物品图标（3D quad 管线） | 图标自身的透明像素；`background` 铺在它下面，`transparent` 保持透明 | 改变画布尺寸，整数倍时原生重绘 |
| `tooltip` | 宽 360，高由内容决定 | 九宫格背景 + 文本 | 不透明 | `width` 改变折行宽度（改变版式），高度始终自算；纵向只认 `scale` |
| `inventory` | 176 × (17 + 18×行数 + 96)（3 行 = 167） | `generic_54` 容器界面 | 不透明 | 整数倍原生重绘 |
| `enderchest` | 176×167 | 同上，固定 3 行 9 列 | 不透明 | 整数倍原生重绘 |
| `item_container` | `hopper` 176×133，其余 176×166 | 各自的原版容器界面 | 不透明 | 整数倍原生重绘 |
| `playerInventory` | 176×166 | `InventoryScreen` 1:1 + 3D 玩家预览 | 不透明 | 整数倍原生重绘 |
| `skin` | 256×384 | 3D 玩家模型 | 透明 | 改变画布，整数倍时原生重绘 |
| `playerlist` | 宽度按列数（最少 360），高由人数决定 | TAB 列表 | 不透明 | `width` 改变版式，不放大；要放大只认 `scale` |
| `map` | 140×140 | 羊皮纸 + 128×128 像素网格 + 装饰 | 不透明 | 缩放整图，不裁剪 |
| `achievement` | 480×96 | advancement toast | 不透明 | 整数倍原生重绘 |

`width`/`height` 只给一个时，另一个按该行的原生画布比例换算，不会拉伸。整数倍会按最终分辨率**原生重绘**（文字按目标像素尺寸光栅化、3D 模型重新光栅化），小数倍率或非整数比则对成品图做最近邻放大；细节见「图片尺寸与放大倍率」。

### 行为与降级规则

- **头颅皮肤优先级**：`item.texture` → `components["minecraft:profile"]` 里内联的皮肤 → 客户端默认皮肤 `entity/player/wide/steve`。只给 `name`、`uuid` 或远端 hash 的 profile 不会联网解析，一律回退到默认皮肤。
- **坏皮肤不失败**：`item.texture` 解不出来（尺寸不对、不是 PNG、路径不存在）时，只在 stderr 打一行 `player head texture fell back to its profile` 并继续走 profile → 默认皮肤，HTTP 仍是 200。
- **单个贴图缺失只影响那一层**：其余层照常绘制；整个物品都没有可用几何时画原版紫黑缺失模型。
- **未知物品 id / 未知模型**：画原版缺失模型，不返回 422。

### player_head 头颅皮肤

`minecraft:player_head` 的皮肤有两条入口，推荐第一条：

```json
{
  "version": "1.21.11",
  "type": "item",
  "output": "base64",
  "data": {
    "id": "minecraft:player_head",
    "texture": "data:image/png;base64,<64x64 或 64x32 的皮肤 PNG>"
  }
}
```

第二条是原版 `minecraft:profile` 组件：`properties[].value` 必须是 **profile JSON 文档的 base64**，而文档里的 `textures.SKIN.url` 又必须是 **`data:image/png;base64,...`**——也就是双层 base64，手工拼非常容易错。下面的片段直接把一份皮肤 PNG 包装成可发送的报文：

```sh
python3 - <<'PY' | curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data-binary @-
import base64, json

png = open("alex.png", "rb").read()          # 64x64 或 legacy 64x32 的皮肤
skin = "data:image/png;base64," + base64.b64encode(png).decode()
profile = {"textures": {"SKIN": {"url": skin}}}
request = {
    "version": "1.21.11",
    "type": "item",
    "output": "base64",
    "data": {
        "id": "minecraft:player_head",
        "components": {
            "minecraft:profile": {
                "properties": [{"name": "textures",
                                "value": base64.b64encode(json.dumps(profile).encode()).decode()}]
            }
        },
    },
}
print(json.dumps(request))
PY
```

四点事实：

1. **只对 `player_head` 生效**：其他物品即使带 `texture` 也会被忽略（不报错）。
2. **legacy 64×32 自动补齐**：左右肢体会按原版方式从右侧镜像出来，不需要自己预处理。
3. **优先级与降级**：`item.texture` 优先于 profile；`texture` 不可用时回退到 profile，再回退到 `entity/player/wide/steve`。
4. **任何物品位置都生效**：`item`、容器槽位、`playerInventory` 的手持物、`tooltip` 的图标都用同一条皮肤解析链。

皮肤值除了内联 `data:image/png;base64,...`（推荐，永远不会有歧义），也可以写成资源树里的贴图 id。**贴图 id 必须带命名空间或 `.png` 后缀**（如 `minecraft:textures/entity/player/wide/alex.png`）：解析器先尝试把值当裸 base64 解码，只用字母数字和 `/` 的短路径（例如 `textures/entity/player/wide/alex`）会被误判成 base64，结果退回默认皮肤。

<!-- params:end -->

## 图片尺寸与放大倍率

`options` 里的 `width`、`height`、`scale` 决定输出图片的大小，三者可以组合：

| 字段 | 语义 |
| --- | --- |
| `width` | 画布宽度（渲染分辨率，≥ 16）。只给宽度时高度按该类型的原生画布比例换算，不会拉伸 |
| `height` | 画布高度（渲染分辨率，≥ 16）。只给高度时宽度同样按原生比例换算 |
| `width` + `height` | 直接使用这对画布尺寸（与该类型原有的拉伸语义一致） |
| `scale` | 成品图的放大倍率，见下表的两种写法；与 `width`/`height` 的画布尺寸相互独立 |

`scale` 支持固定比例与按目标尺寸自动缩放两种写法：

| 写法 | 语义 |
| --- | --- |
| `"scale": 2` / `"scale": {"ratio": 2}` | 固定倍率，允许小数（如 `1.5`）；`0` 与 `1` 表示不放大，上限 16，小于 1 时做最近邻缩小 |
| `"scale": {"width": 800}` | 目标最终宽度：按画布宽度求出倍率，高度等比换算 |
| `"scale": {"width": 800, "height": 600}` | 目标最终尺寸框：取 `min(800/画布宽, 600/画布高)`，把图片等比放进框内，不拉伸 |

**整数倍率按最终分辨率原生重绘**：倍率为整数（`2`、`3`、`4`…，或目标尺寸与该类型原生画布成整数比）时，渲染器直接按该倍率光栅化——文字按目标像素尺寸绘制（CJK 点阵逐 texel 落在设备像素上），3D 模型（`skin`/`playerInventory` 的预览、`item` 图标）按最终分辨率重新光栅化，界面贴图则按整数倍保持方块像素。换句话说 `{"scale": 4}` 的玩家预览是真的 4 倍细节，而不是把 176×166 的图放大 4 倍。

固定布局类型（`item`、`skin`/`player`、容器、`playerInventory`、`map`、`achievement`）的 `width`/`height` 若正好是原生画布的整数倍，同样走原生重绘——同样的版式，更锐利的像素。`tooltip`、`playerlist` 的高度由内容决定、`width` 会改变版式而非放大，因此它们只认 `scale` 倍率。

**小数倍率、非整数目标尺寸**回退为对成品图做最近邻放大：像素行/列宽窄交替，目标框的非约束轴可能出现 ±1px 的取整误差。

各类型的原生画布、背景与 `width`/`height` 的作用见「请求参数总表」里的**每种 type 生成什么图**表。原生高度由内容决定的类型（`tooltip`、`playerlist`）只接受画布宽度，高度仍交给渲染器自算；需要限制最终高度时用 `scale` 的目标框。

最终尺寸仍需满足 `MAX_IMAGE_WIDTH` / `MAX_IMAGE_HEIGHT`：`scale` 目标尺寸或“显式画布 × 固定倍率”超限时在入口以 `400 invalid_image_options` 拒绝，原生画布放大后超限则返回 `422 image_too_large`。

```sh
# 60×60 的方块图标
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","options":{"width":60},"data":{"id":"minecraft:red_shulker_box"}}' \
  -o shulker.png

# 原生 480×96 的成就 toast，整体放大 2 倍 → 960×192
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"achievement","output":"png","options":{"scale":2},"data":{"title":"Stone Age","icon":{"id":"minecraft:stone"}}}' \
  -o achievement.png

# 按目标最终尺寸自动缩放：成就 toast 放进 1000×400 的框 → 1000×200
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"achievement","output":"png","options":{"scale":{"width":1000,"height":400}},"data":{"title":"Stone Age","icon":{"id":"minecraft:stone"}}}' \
  -o achievement-target.png

# 先取 32×32 的画布，再整体放大 2.5 倍 → 80×80
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","options":{"width":32,"scale":2.5},"data":{"id":"minecraft:diamond_sword"}}' \
  -o sword.png
```

## 类型支持矩阵

| type | 真实 Java 后端能力 |
| --- | --- |
| `item` | 1.21.11 item definition、model parent、generated/handheld、3D elements（走 quad 管线并按原版方块面明暗着色，方块图标是标准等轴立方体）、纹理层、select/condition/range dispatch、九类 `minecraft:special` 几何（旗帜 / 床 / 潜影盒 / 头颅与玩家头 / 盾牌 / 潮涌核心 / 饰纹陶罐 / 铜傀儡雕像 / 三叉戟）与箱子族平面贴图、图案与染色组件、damage/glint、资源缺失时画原版缺失模型 |
| `tooltip` | 原版九宫格背景（`gui/sprites/tooltip/background`，border 从 `.mcmeta` 读取）与 advanced 边框（`tooltip/frame`）、`§` 与 `&` 颜色/修饰符前缀、原版行生成顺序（悬停名 → advanced id → lore → 附魔 → 属性修饰符 → 单值组件 → 组件/NBT 计数，lore 只来自 `item.lore`）、按真实文本宽度折行、`icon` 开关 |
| `inventory` | 官方 `generic_54` chest GUI，1–6 行九列、标题、主容器槽、玩家区槽位、堆叠数量 |
| `item_container` | hopper（176×133）、furnace/blast_furnace/smoker、crafting_table、anvil、brewing_stand（176×166）；槽位取自各自原版菜单；未知容器返回 `unsupported_container` |
| `enderchest` | 固定 3×9，与普通三行箱子完全一致（同一 `generic_54` 贴图、配色与槽位，无额外叠加） |
| `playerInventory` | 原版 InventoryScreen 1:1 布局（默认 176×166，面板贴图不缩放）、标题 `(97, 6)`、原版预览视口内的 3D 玩家（26→75 / 8→78，30 像素每方块）、外层与披风、`equipment/*.json` 盔甲分层（染色层、elytra 翼）、手持物 thirdperson 3D 变换（手部高度、躯干前方，special 模型也用 3D 几何）、InventoryMenu 槽位（背包 / 快捷栏 / 装备 / 副手） |
| `skin` | 原版 3D 玩家模型：64×64 classic/slim、legacy 64×32（自动补齐左侧肢体）、外层第二层、原版斗篷 UV、手持物 thirdperson 3D 变换（含在自适应缩放内；special 模型同样使用其 3D 几何） |
| `playerlist` | 原版 TAB 布局：行距 9px、8×8 头像、官方 ping 图标与阈值、header/footer 居中、多列（每列 20 行），最多 500 人，整体 2 倍渲染（字体同步放大） |
| `map` | 原版地图图像：`map/map_background` 羊皮纸背景 + 内缩的完整 128×128 像素网格（默认 140×140，内容 1:1）+ `map/decorations/*` 原版装饰图标，最多 16384 项 pixels 或解码后最多 16384 字节 pixelData、marker rotation/type/name、dimension/locked/scale |
| `achievement` | 官方 advancement toast（160×32 内容，默认放大 3 倍到 480×96）、task/goal/challenge obtained/unobtained frame、icon、标题与描述在 x=30 的第 7/18 行 |

## 字体

文本完全走原版 `FontManager` 的 provider 管线：`assets/minecraft/font/default.json` 依次声明 `space` → `bitmap` → `reference`（`include/unifont`）→ `unihex`，Java renderer 与 Go 简化后端（`internal/font`）解析同一份资源、执行同一套规则。渲染器不再内嵌任何 TTF，服务运行时不联网下载字体。材质包里的同名定义按资源栈从高到低拼接 provider（规则见「材质包（resource packs）」一节），因此包只追加 provider 时，下面这张表描述的原版链路仍然生效。

| provider | 资产 | 行为 |
| --- | --- | --- |
| `space` | `default.json` 内联 | 空格步进 4px，不产生像素 |
| `bitmap` | `textures/font/ascii.png`（ascent 7）、`accented.png`（ascent 10、高 12）、`nonlatin_european.png` | 拉丁与重音字形；保留左空距，只裁掉右侧空列，步进 = 缩放后墨迹宽 + 1。`height` 是字形的字体像素高度（缺省 **8**，与贴图 cell 尺寸无关），`ascent` 缺省 `height - 1`：材质包把 2 倍分辨率的贴图声明成一半高度就能得到正常大小的图标，渲染器按面积覆盖把 cell 降采样进 `height` 高的字形盒 |
| `reference` | `font/include/unifont.json` | 内联 Unifont 子定义（含 `size_overrides`），可继续嵌套 |
| `unihex` | `font/unifont.zip` | 16×16 Unifont 点阵按 2×2 覆盖度降到 8×8，每个像素保留 0/1/2/3/4 五档覆盖度（半透明灰阶）；`size_overrides` 把 CJK 块固定为全角，步进 9 |

`filter`（`uniform` / `jp`）按原版语义求值：默认字体不启用任何选项，因此 `uniform: false` 的 bitmap 组保留、`jp: true` 的日文 Unifont 变体被跳过。中文保留原始 Unicode 码点，不做拼音化、转写或替换，字形来自 `unifont.zip` 的点阵而非矢量字体。

度量与原版一致：传入坐标就是 `GuiGraphics.drawString` 的行顶坐标（基线 = 行顶 + 7），空格 4、普通字形「墨迹宽 + 1」、CJK 全角 9，tooltip 行高 10。`bitmap` 字形的高度由 `height` 决定而不是贴图 cell：cell 与声明的 `height` 不一致时（例如 ItemsAdder 包 11×14 的图标声明 `height: 7`），字形盒是 `height` 高的字体像素盒，宽度为 `ceil(墨迹 texel 宽 / scale)`，`scale = cellHeight / height`，盒内每个像素的覆盖度取该处 texel 的面积占比，步进按缩放后的墨迹宽 + 1 计算；渲染倍率不小于 `scale` 时直接按原始 texel 重采样，2 倍分辨率的图标在 2 倍画布上就是一个 texel 一个设备像素。`bitmap` 贴图的 alpha 只有「有墨 / 无墨」两态（贴图自带透明度不参与覆盖度），颜色则按 `贴图 RGB × 文本颜色` 调制：原版三张贴图是纯白，因此就是一色的文本色，而材质包自带的彩色图标（例如 ItemsAdder 的按键图标）会保留自己的配色，`§f` 时与贴图完全一致，`§7` 等颜色会把整块图标等比压暗。`unihex` 的 16×16 点阵同时保留原始网格与 2×2 归并后的 8×8 视图：1 倍时用 8×8 的 5 档覆盖度（`0`–`4` 四分之一）绘制，1 档与 2 档半透明，就是原版 GUI scale 1 的柔边；**2 倍及以上改用原始 16×16 网格重采样**，2 倍时一个 texel 正好落在 1 个设备像素上（笔画是实色，不再是灰块），3 倍等非整数倍按面积覆盖做一次正确的抗锯齿。上述字体资产由 `prepare` 阶段从官方 asset index 物化到 `data/resources/<version>/vanilla/assets/minecraft/font/`，与其它原版资源一同按 SHA-1 缓存，并在资源根目录写一份 `font-overlay.json` 记录本次物化的文件清单。

客户端 jar 自带的 `font/default.json` 和 `include/{default,space}.json` 只是占位，真正的 Unifont 定义与压缩包只存在于 asset index。因此：

- 服务启动时会按清单检查每个 ready 版本的资源树，缺文件就用本地对象缓存补齐（不联网），日志出现 `msg=restored missing font assets`；
- 连对象缓存也缺失时该版本不再是 ready，渲染返回 `409 resources_not_ready`，重新 `PUT /api/v1/resources/<version>` 即可；
- 渲染成功但渲染进程在 stderr 上报告了缺字（例如资源包只覆盖了部分字形）时，服务端会以 `msg=renderer diagnostics` 记录最多 4 行，而不是让图片静默变成缺字方块。

## skin 示例

`skin.texture` 和 `skin.cape` 只能是经过尺寸校验的 inline PNG 或已登记资源路径，不能是 HTTP URL。支持 64×64、legacy 64×32 skin（左侧手臂/腿部会按原版方式从右侧镜像补齐）；cape 支持 64×32 或 22×17，按原版斗篷部件（宽 10、高 16、厚 1）挂在背后。

渲染使用原版 `PlayerModel` 几何与 `ModelPart.Cube` 的贴图布局：每单位 1 像素皮肤（16 单位 = 1 方块），3/4 视角正交相机（绕 Y 轴 30°、俯角 20°），逐面按原版 shader 公式着色（`min(1, (max(0,l0·n)+max(0,l1·n))*0.6+0.4)`）。`outerLayer` 控制头发/外套/袖子/裤腿第二层。

`mainHand` / `offhand` 用原版 `ItemInHandLayer` 的挂载链渲染成 3D 几何，而不是贴图：在原版实体模型空间中走 `translateToHand(arm)` → `Rx(-90°)` → `Ry(180°)` → `T(±1/16, 0.125, -0.625)`，再叠加模型 JSON `display.thirdperson_righthand` / `thirdperson_lefthand` 的 `T·R(rotationXYZ)·S·T(-0.5,-0.5,-0.5)`（`thirdperson_lefthand` 缺省时按原版回退到 `thirdperson_righthand`），最后整体乘上原版模型空间到场景空间的转换 `F = T(0, 24, 0)·Rx(180°)`（原版模型空间 y 向下、原点在颈部、模型朝 -z；场景空间 y 向上、脚底为原点、模型朝观察者 +z）。`generated`/`handheld` 模型按原版 `ItemModelGenerator` 生成 16×16×1 的底板与逐像素 1px 轮廓条（z 7.5–8.5），带 `elements` 的模型直接使用元素面几何；`gui_light: front` 的模型全亮。手持物与玩家共用同一深度排序，因此与原版一致地落在手部高度、躯干前方（`player` / `skin` 的自适应缩放把手持物一并框入画布；`playerInventory` 的预览视口固定，手持物不影响模型缩放）。

```json
{
  "version": "1.21.11",
  "type": "skin",
  "output": "base64",
  "data": {
    "texture": "data:image/png;base64,<validated-png>",
    "cape": "data:image/png;base64,<validated-cape>",
    "model": "slim",
    "outerLayer": true
  }
}
```

## tooltip 说明

`tooltip` 按原版 1.21.11 `TooltipRenderUtil` 渲染：

- **几何**：内容区 = 文本块（宽 = 最长行折叠后的宽度，高 = 行数 × 10，单行时再减 2）；背景九宫格铺在内容区各外扩 12px 的矩形上，border 从 `gui/sprites/tooltip/background.png.mcmeta` 读取（缺省 9）；`advanced: true` 时叠加 `gui/sprites/tooltip/frame`（缺省 border 10）。文本起点为背景左上 + 12，每行步进 10px，标题行之后额外留 2px，文本始终带阴影。
- **样式**：所有文本支持 `§` 颜色与修饰符（`§0`–`§9`、`§a`–`§f`、`§l` 粗体、`§o` 斜体、`§n` 下划线、`§m` 删除线、`§k` 混淆、`§r` 重置）。`§k` 为确定性渲染（按原字符绘制），不做随机化。
- **行生成顺序**（未显式给出 `title` 时，与 `ItemStack.getTooltipLines` 一致）：悬停名（`title` > `item.name` > `components["minecraft:custom_name"]` > `en_US` 语言文件 `item.minecraft.*`/`block.minecraft.*` > id 路径，颜色按 `components["minecraft:rarity"]`，自定义名斜体）→ `advanced` 时的物品 id（深灰）→ lore（只来自 `item.lore` / `components["minecraft:lore"]`，位置固定，缺省 `§5` 斜体；没有单独的 tooltip 行列表）→ 附魔（`components["minecraft:enchantments"]`/`stored_enchantments` 的 `{levels:{id:等级}}` 或简化字段 `item.enchantments` 的 `"sharpness 5"`/`"sharpness V"`，灰色，等级 > 1 追加罗马数字）→ 属性修饰符（`components["minecraft:attribute_modifiers"].modifiers[]` 按 `slot` 分组，表头取 `item.modifiers.<slot>`，数值用 `#.##` 格式，蓝色）→ 单值组件（`unbreakable`、`dyed_color`，`advanced` 时的耐久度）→ `advanced` 时未识别组件的 `minecraft:<path>: <json>` dump 与 `item.components` 计数、`nbt` 存在时的 `item.nbt_tags` 计数。文案来自 `assets/minecraft/lang/en_us.json`。
- **正文行**：`tooltip` 没有独立的 `lines` 字段。需要自定义正文时写进 `item.lore`（最多 128 行、每行 1024 字符），每行可自带 `§`/`&` 格式码覆盖默认的紫色斜体；只要 `title` + `item.lore` 就足以渲染一个没有物品 id 的纯文本提示框。
- **校验**：`item.id` 与 `title` 至少给出一个，否则返回 `400 invalid_data`。
- **折行**：按真实文本宽度在空格处折行；单个单词超过可用宽度时按原版 `Font.split` 规则硬切。可用宽度 = `options.width`（缺省 360）− 24（`icon` 开启时再减去图标占位）。
- **图标**：默认不绘制（与原版一致）；`icon: true` 且提供了 `item` 时在左侧绘制 72px 物品图标。
- **尺寸**：`options.height` 缺省为 0，由内容自算（行数 × 10 + 2 + 24）；显式给出时使用该高度，九宫格会相应拉伸。

```json
{
  "version": "1.21.11",
  "type": "tooltip",
  "output": "base64",
  "data": {
    "item": {
      "id": "minecraft:diamond_sword",
      "lore": ["Forged in the deep"],
      "enchantments": ["minecraft:sharpness 5"],
      "components": {
        "minecraft:unbreakable": {},
        "minecraft:attribute_modifiers": {
          "modifiers": [
            {"type": "minecraft:attack_damage", "amount": 7, "operation": "add_value", "slot": "mainhand"}
          ]
        }
      }
    },
    "advanced": true
  },
  "options": {"width": 420}
}
```

## 特殊容器示例

`container` 支持 `hopper`、`furnace`/`blast_furnace`/`smoker`、`crafting_table`、`anvil`、`brewing_stand`。每个界面都按反编译的 1.21.11 屏幕实现：画布尺寸、标题列与槽位坐标全部取自对应类。

| 容器 | 贴图 | 画布 | 标题 x | 容器槽位（内容坐标，按菜单顺序） |
| --- | --- | --- | --- | --- |
| `hopper` | `hopper` | 176×133 | 8 | 5 格 `(44 + 18i, 20)` |
| `furnace`/`blast_furnace`/`smoker` | 同名 | 176×166 | 居中 | 输入 `(56, 17)`、燃料 `(56, 53)`、输出 `(116, 35)` |
| `crafting_table` | `crafting_table` | 176×166 | 29 | 3×3 从 `(30, 17)`、结果 `(124, 35)` |
| `anvil` | `anvil` | 176×166 | 60 | `(27, 47)`、`(76, 47)`、`(134, 47)` |
| `brewing_stand` | `brewing_stand` | 176×166 | 居中 | 药水 `(56, 51)`/`(79, 58)`/`(102, 51)`、燃料 `(79, 17)`、配料 `(17, 17)` |

容器槽位之后紧跟玩家区：`hopper` 从菜单槽 5 起排在 `(8, 51)`，熔炉/工作台/铁砧/酿造台从各自槽位数起排在 `(8, 84)`；每个界面都是 27 格背包加 58px 下方的 9 格快捷栏。未知容器名返回 `unsupported_container`。

```json
{
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
}
```

`inventory` 的槽位语义相同：菜单槽 0 到 `rows*9-1` 属于容器区，之后是玩家背包区。箱子族的画布高度为 `17 + rows*18 + 96`（3 行 167），槽位坐标是 `generic_54` 贴图的坐标：容器区 `(8 + 18c, 18 + 18r)`，玩家背包第一行 `(8, 31 + rows*18)`，快捷栏再向下 58px。标题按原版画在 `(8, 6)`，堆叠数量画在槽位右下角（`x + 17 - 宽度, y + 9`，白色带阴影）。

`enderchest` 是同一套渲染的 3 行固定版本：原版末影箱界面就是普通三行容器，因此输出与 `inventory` 的 3 行结果逐像素一致，没有紫色叠加或额外标识。

## playerInventory 与 playerlist 说明

`playerInventory` 复用同一套原版玩家模型，并按原版装备层绘制穿戴物：

- `armor` 前四项依次是头盔、胸甲、护腿、靴子；材质名取自物品 id（`diamond_chestplate` → `diamond`），贴图与染色由 `assets/minecraft/equipment/<材质>.json` 的 `layers.humanoid` / `layers.humanoid_leggings` 决定（`dyeable` 层使用物品 `components["minecraft:dyed_color"]`，缺省用 `color_when_undyed`）。几何按原版 `CubeDeformation` 膨胀（头盔/胸甲/靴子 1.0，护腿 0.5），并按槽位只渲染对应部件；缺失定义返回 `422`（`missing equipment definition: <材质>`）。
- 胸甲槽为 `minecraft:elytra` 时改画 `equipment/elytra.json` 的 `wings` 层（有披风时使用玩家披风贴图）。
- `mainHand` / `offhand` 用原版 `ItemInHandLayer` 挂载链以 3D 几何绘制在左右手上（主手取 `thirdperson_righthand`、副手取 `thirdperson_lefthand`），经模型空间转换后落在手部高度、躯干前方，与玩家模型共用同一深度排序。

`playerInventory` 的整体布局与原版 `InventoryScreen` 一致：默认输出 **176×166**，`gui/container/inventory` 贴图 1:1 绘制；标题按原版 `titleLabelX = 97`、行 6 绘制（避开左侧的合成网格区域）；3D 玩家渲染在原版预览视口内（`x` 26→75、`y` 8→78，缩放 30 像素/方块，脚底对齐视口中心 + `(bbHeight/2 + 0.0625) × 30`，视角与 `skin` 相同的 30°/20°）；槽位沿用原版 `InventoryMenu` 坐标（背包菜单槽 0–26 在 `(8 + 18c, 84 + 18r)`、快捷栏 27–35 在 `(8 + 18c, 142)`、装备 `(8, 8 + 18i)`、副手 `(77, 62)`）。

手持物按原版 `ItemInHandLayer` 链路计算，但整条链在乘入场景空间之前仍处于原版实体模型空间：原版模型空间 y 向下、原点在颈部（脚底 +24px）、模型朝 -z，而玩家模型、披风与装备层所在的场景空间 y 向上、脚底为 0、模型朝观察者 +z，两者相差绕 x 轴半圈 `F = T(0, 24, 0)·Rx(180°)`。乘上 F 后，手持物与原版一致地挂在手心高度、位于躯干之前（默认 3/4 视角下主手与副手物品都可见），而不是落到躯干背后。显式传入 `options.width` / `options.height` 且正好是 176×166 的整数倍时，整张界面（含 3D 预览）按该倍率原生重绘；非整数比则退化为整图最近邻拉伸，语义与 `inventory` / `enderchest` 一致。`options.scale` 为整数倍时同样原生重绘，因此放大后的皮肤是重新光栅化的，而不是把 1 倍的预览放大。

`playerlist` 使用原版 TAB 列表几何（9px 行距与 8×8 头像，本服务按 2 倍渲染以便在 Discord 等场景查看，字体同样放大 2 倍）：每个玩家必须提供 `texture`（与 `skin` 相同的校验，不能是网络 URL），头像取皮肤 `(8,8)` 脸部与 `(40,8)` 外层，右侧 ping 图标按原版阈值选择 `gui/sprites/icon/ping_5..ping_1`（`<150`/`<300`/`<600`/`<1000`），`ping <= 0` 或缺失时使用 `ping_unknown`；每列最多 20 行、多列从左到右、header/footer 每行居中。`columns` 缺省为 0（自动：>40 人 3 列、>20 人 2 列、否则 1 列），只接受 0–16；渲染器只画得下 1–8 列，9–16 会因画布宽度超过 `MAX_IMAGE_WIDTH` 而以 `422` 失败。

`achievement` 按原版 `AdvancementToast` 渲染 160×32 的 toast 内容，再整图放大到默认 480×96：标题在 `(30, 7)`、描述在 `(30, 18)`，图标与 frame 贴图沿用原版位置。

## map 示例

输出与原版地图图像一致：`minecraft:map/map_background` 羊皮纸背景铺满整图，128×128 像素网格按 `size / 23.3333` 内缩后铺进内容区（默认输出 140×140，内容与地图像素 1:1；`options.width/height` 只缩放整图，不会裁剪），装饰使用 `minecraft:map/decorations/*` 原版贴图并按 `rotation * 22.5°` 顺时针旋转、图标大小取原版的 96/1120 比例。

`pixels` 使用 `index = z*128+x` 的 16384 项扁平数组，`0` 表示透明；也可用 `pixelData`（`base64:` 前缀，解码后最多 16384 字节）。marker 的 `type` 取原版装饰名：`player`、`frame`、`target_point`、`red_x`、`<颜色>_banner`（16 色）、`plains_village`/`desert_village`/`jungle_temple`/`swamp_hut`/`snowy_village`/`savanna_village`/`taiga_village`、`mansion`、`ocean_monument`、`trial_chambers`、`red_marker`、`blue_marker`、`player_off_map`、`player_off_limits`，以及别名 `banner`→`white_banner`、`target`→`target_point`、`village`→`plains_village`、`monument`→`ocean_monument`；未收录的名称返回 `422 render_failed`（`unknown map marker type: <type>`），不会静默替换。`dimension` 与 `locked` 会在角落绘制注解文字（本服务的辅助信息，非原版元素）。

下面的示例用 `base64:` 传输完整 16384 字节，避免在文档中手写超长 JSON：

```sh
python3 - <<'PY' | curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data-binary @-
import base64, json
pixels = bytes(1 + ((x // 16 + y // 16) % 6) for y in range(128) for x in range(128))
request = {
    "version": "1.21.11",
    "type": "map",
    "output": "base64",
    "data": {
        "pixelData": "base64:" + base64.b64encode(pixels).decode(),
        "markers": [{"x": 10, "z": 10, "rotation": 4, "type": "player", "name": "Spawn"}],
        "scale": 2,
        "dimension": "minecraft:overworld",
        "locked": True,
    },
}
print(json.dumps(request, ensure_ascii=False))
PY
```

## 颜色码（`§` 与 `&`）

所有文本入口（容器标题、`playerInventory` 标题、playerlist 的 header/footer 与玩家名、`map` 的 dimension 与标记名、`achievement` 的标题与描述、tooltip 的每一行）都按同一套原版 legacy 格式解析：`0-9a-f` 十六色（原版调色板）、`l` 粗体、`o` 斜体、`n` 下划线、`m` 删除线、`k` 混淆（确定性渲染，不做随机化）、`r` 重置。原版只认 `§`（U+00A7）；`&` 是本服务的兼容扩展，两者都作为前缀接受，**只有后跟合法码字符时才算格式码**，因此字面量 `&` 与 `A& B` 原样保留（消费方不必再自行把 `&` 转成 `§`）。

颜色还可以写成任意 RGB：`§x§R§R§G§G§B§B`（例如 `§x§6§6§9§9§F§F` 为 `0x6699FF`，写法取自原版 `ChatFormatting` 的十六进制约定）。标记 `x`/`X` 本身不区分大小写，六组「前缀 + 一位十六进制数字」的前缀可以混用 `§` 与 `&`、数字大小写不限；它只改颜色、不动 `l/o/n/m/k` 这些修饰符，`§r` 之后同样回到入口默认色。**只有六组齐全时才算一个颜色码**，缺任意一组就整段按字面处理（前缀与 `x` 原样绘制，后面若另有完整单字符码仍各自生效）。

未带码的文本使用该入口在游戏中用的颜色（容器标题 `0x404040`、tooltip/playerlist/achievement/map 为白色、`achievement` 完成态标题为 `0xFFFF55`），`§r` 也回到这个颜色。格式码不计入宽度：容器标题的居中、playerlist 的列宽与 header 居中、tooltip 的折行都用去码后的宽度（十六进制码同样不计宽）。全部文本均带原版字形盒几何（行顶到底部 7px × 缩放），容器标题与地图标签按原版不带阴影，tooltip 与 playerlist 带阴影。`§l` 与原版一致地「把字形向右膨胀一像素」：覆盖度取本像素与左邻像素的最大值、字距每字符 +1px，所以加粗只让笔画变粗，不会出现重影或普通字形没有的额外灰阶。

## special 物品模型

`minecraft:special` 的模型定义在客户端代码里而不是 model JSON 中，本服务按 1.21.11 反编译结果逐盒直译几何与贴图区域，语义与客户端一致（同一个 `texOffs` + `PartPose` 盒列表、同一套贴图展开、同一个 `display` 变换）：

| 族（模型定义 `model.type`） | 几何与贴图 |
| --- | --- |
| `banner` | 旗杆 + 横杆（`entity/banner_base`）与旗面：先按模型的 `color` 染 `entity/banner/base`，再按 `banner_patterns` 的每个图案染 `entity/banner/<pattern>`；`base_color` 组件覆盖模型颜色 |
| `bed` | 床头 / 床尾两半（`entity/bed/<texture>`），按 `BedRenderer` 的 `translate(0, 0.5625, 0/-1) → rotateX(90°)` 摆放 |
| `head` | `kind` 对应 `entity/{skeleton, creeper, zombie, piglin, enderdragon}/…`，`texture` 字段可覆盖；8³ 头颅 + 0.25 膨胀覆盖层（humanoid kind），固定 `yRot = 180°` 面对观察者 |
| `player_head` | 同上的人类头层，皮肤按优先级取 `item.texture`（推荐）→ `components["minecraft:profile"]` 里内联的皮肤 → 客户端默认皮肤 `entity/player/wide/steve`；不联网解析 name / uuid / 远端 hash，详见「player_head 头颅皮肤」 |
| `shield` | 盾面 + 握把（`entity/shield_base_nopattern` / `entity/shield_base`），有图案时按 `banner_patterns` 逐层染 `entity/shield/base` 与 `entity/shield/<pattern>` |
| `conduit` | 未激活状态的外壳（6³，`entity/conduit/base`）+ `translate(0.5, 0.5, 0.5)`；笼、风与眼睛三层属于方块实体的激活状态，不在物品图标里 |
| `decorated_pot` | 颈 + 盖 + 四面侧壁（`entity/decorated_pot/*`），`pot_decorations` 按原版 back/left/right/front 顺序选陶片图案 `*_pottery_pattern`，没有陶片的侧面用 `decorated_pot_side` |
| `copper_golem_statue` | 站立姿势的铜傀儡盒列表（`texture` 指向氧化程度贴图）；`sitting` / `running` / `star` 三种姿势尚未直译，暂用站立姿势 |
| `trident` | 杆 + 基座 + 三叉（`entity/trident`）；GUI 上下文的 `select(display_context)` 会选中平面模型 `item/trident`，因此这条几何用于手持 |
| `shulker_box` | 关闭状态的潜影盒：盒盖 `lid`（`texOffs(0, 0)`，16×12×16）与盒身 `base`（`texOffs(0, 28)`，16×8×16）两个盒子叠出 1×1×1 方块，`texture` 字段直接给出染色后的贴图（`entity/shulker/shulker` 为未染色，`entity/shulker/shulker_red` 等为 16 色），`display.gui` 的 30°/45° 与 0.625 让它在图标里呈等轴 3D 方块；盒盖底边 4 单位透明、盒身顶边透出的 8 像素凸起就是原版贴图的卡扣 |
| `chest` / `ender_chest` | 仍是原版平面实体贴图（`entity/chest/normal`、`entity/chest/ender`），与之前版本逐像素一致 |

**所有** item 图标共用一条 3D 管线：普通模型 JSON 里的 `elements`（方块、家具）、上表的 `minecraft:special` 几何、以及原版缺失模型，都按「`display.gui` 的 `T·R(rotationXYZ)·S·T(-0.5,-0.5,-0.5)` 摆姿势 + 正交直视相机（1 单位 = 1 像素、物品中心落在图标中心）」在最终分辨率上逐像素光栅化，面按深度排序、逐面按 `uv` 采样，因此方块不会被按外接框拉伸、棱边是直线。

`uv` 是**模型空间 0..16 的纹理坐标**，不是贴图像素下标：0..16 映射到整张贴图，所以 32×32、96×96、112×112 的高清贴图会整张参与采样（Blockbench 写进模型的 `texture_size` 只是编辑器元数据，原版不读，这里也不参与换算），小数 `uv` 也按原值参与采样。面的 `rotation`（0/90/180/270）按原版 `BlockFaceUV` 的角点编号就地旋转取样区域，不移动几何。带动画元数据的贴图（`.png.mcmeta` 的 `animation`）取**第一帧**作为静态图标，不会把整条竖表拉到一个面上。

元素的 `rotation`（`angle` / `axis` / `origin` / `rescale`）按原版 `FaceBakery#rotatePart` 在 `display` 姿态之前应用——自定义武器包几乎每个元素都带旋转，忽略它会把盒子叠在原位、把模型主体挡掉；`rescale` 用原版的两个系数（22.5° 与其余角度）。元素的 `shade: false` 与 `gui_light: front` 一样不参与面明暗。

`elements` 模型的面明暗取自原版 `ModelBlockRenderer` 的面方向表——上 `1.0`、下 `0.5`、南北 `0.8`、东西 `0.6`（按模型面自己的方向，不是按变换后的法线），`gui_light: front` 的模型不参与——所以标准 `display.gui` 姿态下的整方块是「上表面最亮、两侧一深一浅」的等轴立方体，这也是原版槽位图标的观感。声明了 `tintindex` 的面仍叠一层 42/255 的白色（原版染色表另行排期）。

手持走 `thirdperson_righthand` / `thirdperson_lefthand` 与 `ItemInHandLayer` 链条，深度排序与遮挡和玩家模型共用。

## 自定义物品（`customData`）

原版物品的图标由 item definition 决定，而 ItemsAdder 这类插件用的是「同一个基础材料 + 一个 `custom_model_data` 数值」来表达一件自定义物品。本服务直接把这条映射读到渲染链路里：请求的物品带上 `customData`，就会按用户包里的贴图与模型几何绘制，而不是画原版材料。

```sh
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","data":{"id":"minecraft:paper","customData":1134}}' \
  -o starfish.png
```

- **定位键 = 基础材料 + 数值**：`id` 的命名空间与 `customData` 组合成索引键（`minecraft:paper` + `1134` → `PAPER#1134`），与 `items_ids_cache.yml` 里的 `<MATERIAL>: {<namespace>:<name>: <cmd>}` 对应。同一数值在不同材料下是不同物品（`PAPER 1134` 与 `FISHING_ROD 1134` 互不相干），所以调用方必须传真实的基础材料。
- **`customData` 就是原版 `custom_model_data`**：它既是自定义内容索引的键（基础材料 + 数值），也是模型 `overrides` 的 `custom_model_data` predicate 与 item definition 的 `select` / `range_dispatch` 读的那个数。`customModelData` 只是同一个数值的旧拼写，保留兼容；两者都传时以 `customData` 为准。
- **任何物品位置都生效**：`item`、`tooltip.item`、`inventory/item_container/enderchest.slots[].item`、`playerInventory.armor/mainHand/offhand`、`achievement.icon`。
- **绘制的是贴图 + 模型几何**：模型 JSON 的 `parent` 链会被逐层合并（遇到 `minecraft:` 父模型即停，并保留它以便沿用原版的 `display` 手部变换），`textures` 里的自定义贴图按需读文件并内联成 data URI 传给渲染后端（同名 `.png.mcmeta` 一并带上，用于取动画贴图的第一帧）。因此 `generated` 平面图标、带 `elements` 的 Blockbench 家具模型、以及**手持物品**都能画；高清贴图按模型 `uv` 的 0..16 坐标整体采样，不会被截成左上角。
- **未命中不报错**：材料不匹配、索引里没有这个数值、配置里没有这个物品、贴图缺失，都会回退成原版物品并记一条 `WARN`（每个键一次），HTTP 仍然返回 200。

### 目录与索引

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| `CUSTOM_CONTENTS_DIR` | `<DATA_DIR>/contents` | 每个子目录是一个包，布局同 ItemsAdder `contents/`；不存在时启动会创建空目录并视为空索引 |
| `CUSTOM_ITEMS_CACHE` | `<DATA_DIR>/items_ids_cache.yml` | ItemsAdder `storage/items_ids_cache.yml` 的副本或软链 |
| `MAX_CUSTOM_ITEMS` / `MAX_CUSTOM_TEXTURE_BYTES` / `MAX_CUSTOM_LAYERS` | `50000` / `8 MiB` / `16` | 索引条目、单贴图、单物品贴图层数上限 |

```
data/
  items_ids_cache.yml            # 材料 → (namespace:item → custom_model_data)
  contents/
    fishing_pack/
      configs/*.yml              # info.namespace + items.<name>.resource.{material,generate,textures,model_path}
      resourcepack/fishing_pack/{models,textures}/…
    icon_pack/
      configs/items.yml          # generate: true + textures: [item/1]
      textures/item/1.png        # 没有 models/ 目录
```

解析器只读 `configs/**` 与 `items_packs/**` 下的 `*.yml`（深度 ≤ 4），只理解 ItemsAdder 配置用到的 YAML 子集：缩进映射、块序列（可与键同缩进，ItemsAdder 导出的配置多为此种写法，CRLF 同样处理）、行内 `[a, b]`、引号字符串与 `#` 注释；锚点 / 块标量 / 行内映射这类结构不会中断整包，最坏是那条记录被忽略。资源目录支持实测存在的几种布局：`resourcepack/assets/<ns>/…`、`resource_pack/assets/<ns>/…`、`assets/<ns>/…`、`resourcepack/<ns>/{models,textures}/…`、以及直接平铺的 `textures/`、`models/`。zip / rar 包不解析（ItemsAdder 装上后本来就会解压成目录）。

`types`（材料、贴图、模型）全部由用户手动维护，服务只读取并在需要时内联贴图；索引在启动时后台预热，命中过缓存后单个物品的首帧开销只发生一次。

## 资源路径（sources / reload）

`GET /api/v1/resources/sources` 返回服务当前真正读取的每一类路径与状态，用来判断「图为什么不一样」是缺资源还是缺配置：

```sh
curl http://localhost:4666/api/v1/resources/sources
```

```json
{
  "dataDir": "/app/data",
  "resourcePacks": {
    "directory": "/app/data/resourcepacks",
    "cacheRoot": "/app/data/resources/.packs",
    "status": "ready",
    "packs": [{
      "file": "Base.zip",
      "priority": 1,
      "addedAt": "2026-01-01T00:00:00Z",
      "root": "/app/data/resources/.packs/pack-1a2b3c4d5e6f7788",
      "files": 312,
      "packFormat": 46,
      "status": "loaded"
    }]
  },
  "versions": [{
    "version": "1.21.11",
    "status": "ready",
    "resourceRoot": "/app/data/resources/1.21.11",
    "vanillaRoot": "/app/data/resources/1.21.11/vanilla",
    "addonRoot": "/app/data/resources/1.21.11/addon",
    "packRoots": ["/app/data/resources/.packs/pack-1a2b3c4d5e6f7788"],
    "font": {
      "definition": "/app/data/resources/1.21.11/vanilla/assets/minecraft/font/default.json",
      "definitionPresent": true,
      "unifont": "/app/data/resources/1.21.11/vanilla/assets/minecraft/font/unifont.zip",
      "unifontPresent": true,
      "manifest": "/app/data/resources/1.21.11/font-overlay.json",
      "manifestPresent": true
    }
  }],
  "customContents": {
    "root": "/app/data/contents",
    "itemsCache": "/app/data/items_ids_cache.yml",
    "status": "ready",
    "cachePresent": true,
    "scannedAt": "2026-01-01T00:00:00Z",
    "packs": [{"name": "fishing_pack", "root": "…/fishing_pack", "namespaces": ["fishing_pack"], "items": 143, "configs": 3}],
    "items": {"cached": 3164, "withDefinition": 2534, "resolved": 12},
    "warnings": []
  }
}
```

`resourcePacks` 与 `GET /api/v1/resources/packs` 返回同一份报告（目录、包文件、优先级、解包路径）；每个版本的 `packRoots` 是**实时**的材质包栈，与版本树无关，`PUT` 调整顺序后这里立刻反映。

`items.cached` 是索引里能读到的映射条数，`withDefinition` 是其中真正有包配置支持的条数，`resolved` 是本次进程已经成功解析过（并缓存）的物品数——`resolved` 按需增长，避免为了统计去读走整盘几千张贴图。

`POST /api/v1/resources/custom/reload` 重新扫描用户包与索引并返回同一份 `customContents`。用户手动加了包、改了 `items_ids_cache.yml` 之后调用它即可生效，不需要重启服务；重建会丢弃已缓存的模型与贴图。

## 资源缺失时的行为

渲染不再因为资源缺失整体失败（保持 `422` 的只有请求结构性错误，例如 unknown operation、`item.id` 缺失、未知 `item_container`、未知地图标记类型）：

- 未知物品 id、缺失的 model JSON、没有可用几何或整层透明的模型、未直译的 special 族：该物品画**原版缺失模型**（16³ 立方体 + 生成的紫黑缺失贴图，即 `0xFFF800F8` / 黑色棋盘），并把原因写到 stderr；
- 单个贴图缺失或不是合法 PNG：**只有那一层**用缺失贴图继续渲染，其余层照常；
- 容器 / 背包 / tooltip / achievement 里单个槽位渲染异常只影响该槽位（同样画缺失模型），整图仍然产出；
- 手持物失败时该手持物画缺失模型。

## 调试日志（`DEBUG`）

`DEBUG=true` 时日志级别升到 debug，并为每个请求打一行 `msg=request` 的 JSON 日志：`method`、`path`、`remote`、`status`、`duration`、`contentLength`、`bodyBytes`、`bodyTruncated`、`payload`。`payload` 就是收到的原始报文（含 `POST /api/v1/render` 的完整 JSON），便于排查客户端到底发了什么；报文是在 handler 读取过程中顺带复制的，不会被缓存或二次读取。

单个报文的日志上限为 64 KiB（inline 皮肤等大字段可能触顶），超出时 `bodyTruncated=true`、`bodyBytes` 仍是真实字节数。默认 `DEBUG=false`，不打印任何报文内容。

```sh
DEBUG=true ./dist/minecraft-view-generator2
# {"time":"...","level":"DEBUG","msg":"request","method":"POST","path":"/api/v1/render",...,"payload":"{\"version\":\"1.21.11\",...}"}
```

## 健康与 fallback

服务没有独立的健康检查接口（原 `/healthz` 已删除）。存活探针读 `GET /api/v1/versions`：它不需要 API key，恒返回 `200` 与版本列表，只要 HTTP 栈与资源管理器可用就算存活；`docker-compose.yml` 的 healthcheck 就是这么配的。

渲染后端是否就绪由渲染请求本身报告：真实 Java 命令不可执行且未显式设置 `ALLOW_FALLBACK_RENDERER=true` 时，`POST /api/v1/render` 返回 `503 renderer_unavailable`（`BackendInfo` 的 `status`/`error` 就是原因）。fallback 仅用于测试/故障诊断，不代表 Minecraft 资源渲染结果。

常见错误：`409 resources_not_ready`、`422 render_failed`、`503 renderer_unavailable`、`400 invalid_data` / `invalid_image_options`。
