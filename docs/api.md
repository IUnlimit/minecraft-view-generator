# Minecraft View Generator API

服务固定使用 Minecraft `1.21.11`。默认后端是 Java 21 无头 CPU renderer，读取 Mojang client JAR 与资源包栈；未准备资源时不会返回伪造图片。

## 启动与接口文档

在仓库根目录执行 `./run.sh` 即可一键运行。脚本会在 `dist/minecraft-view-generator2` 不存在时自动执行构建，并默认监听 `0.0.0.0:4666`；也可以用 `BUILD=1 ./run.sh` 强制重新构建。脚本会把数据目录和 Java renderer 路径固定为仓库内的绝对路径，因此可以从任意当前目录调用。

交互式 Scalar API Reference 位于 `http://127.0.0.1:4666/docs`，`/scalar` 是同一个页面的别名；原始 OpenAPI 文档位于 `http://127.0.0.1:4666/api/v1/openapi.json`。Scalar 页面通过 CDN 加载前端，浏览器访问文档页时需要能够访问 jsDelivr。

`POST /api/v1/render` 的 requestBody 内置了十种 type 的可直接发送报文（Scalar 示例切换器：`item`、`tooltip`、`inventory`、`item_container`、`enderchest`、`playerInventory`、`skin`、`playerlist`、`map`、`achievement`），统一使用 `output=base64`。示例中的皮肤是服务启动时生成的 64×64 占位贴图，因此可以直接执行；渲染前仍需先准备资源。

OpenAPI 文档随二进制内嵌生成：调整接口后执行 `make build`（或让 `./run.sh` 自动构建）并重启服务，即完成文档重新生成，随后核对 `/docs` 与 `/api/v1/openapi.json`。

## 资源准备

```sh
curl -X PUT http://localhost:4666/api/v1/resources/1.21.11
curl http://localhost:4666/api/v1/resources/1.21.11
curl http://localhost:4666/healthz
```

资源层级为 vanilla → addon → 用户 `RESOURCE_PACK_ROOT` 中按名称排序的 pack，后者覆盖前者。

## 统一渲染请求

所有类型都使用：`version`、`type`、`output`、`data`、可选 `options.width/height/transparent/background/scale/frame`。

```sh
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","data":{"id":"minecraft:diamond_sword","count":1}}' \
  -o item.png
```

`output=png` 返回 `image/png`；`output=base64` 返回 `{ "mimeType":"image/png", "encoding":"base64", "data":"...", "width":n, "height":n }`。

## 类型支持矩阵

| type | 真实 Java 后端能力 |
| --- | --- |
| `item` | 1.21.11 item definition、model parent、generated/handheld、3D elements、纹理层、select/condition/range dispatch、支持 shulker/chest special、damage/glint |
| `tooltip` | 原版九宫格背景（`gui/sprites/tooltip/background`，border 从 `.mcmeta` 读取）与 advanced 边框（`tooltip/frame`）、`§` 颜色与修饰符、原版行生成顺序（悬停名 → advanced id → lore → 附魔 → 属性修饰符 → 单值组件 → 组件/NBT 计数）、按真实文本宽度折行、`icon` 开关 |
| `inventory` | 官方 `generic_54` chest GUI，1–6 行九列、标题、主容器槽、玩家区槽位、堆叠数量 |
| `item_container` | hopper（176×133）、furnace/blast_furnace/smoker、crafting_table、anvil、brewing_stand（176×166）；槽位取自各自原版菜单；未知容器返回 `unsupported_container` |
| `enderchest` | 固定 3×9，与普通三行箱子完全一致（同一 `generic_54` 贴图、配色与槽位，无额外叠加） |
| `playerInventory` | 原版 InventoryScreen 1:1 布局（默认 176×166，面板贴图不缩放）、标题 `(97, 6)`、原版预览视口内的 3D 玩家（26→75 / 8→78，30 像素每方块）、外层与披风、`equipment/*.json` 盔甲分层（染色层、elytra 翼）、手持物 thirdperson 3D 变换（手部高度、躯干前方）、InventoryMenu 槽位（背包 / 快捷栏 / 装备 / 副手） |
| `skin` | 原版 3D 玩家模型：64×64 classic/slim、legacy 64×32（自动补齐左侧肢体）、外层第二层、原版斗篷 UV、手持物 thirdperson 3D 变换（含在自适应缩放内） |
| `playerlist` | 原版 TAB 布局：行距 9px、8×8 头像、官方 ping 图标与阈值、header/footer 居中、多列（每列 20 行），最多 500 人，整体 2 倍渲染（字体同步放大）；`gameMode` 接受校验但不绘制 |
| `map` | 原版地图图像：`map/map_background` 羊皮纸背景 + 内缩的完整 128×128 像素网格（默认 140×140，内容 1:1）+ `map/decorations/*` 原版装饰图标，最多 16384 项 pixels 或解码后最多 16384 字节 pixelData、marker rotation/type/name、dimension/locked/scale |
| `achievement` | 官方 advancement toast（160×32 内容，默认放大 3 倍到 480×96）、task/goal/challenge obtained/unobtained frame、icon、标题与描述在 x=30 的第 7/18 行 |

## 字体

英文字符使用 [tryashtar/minecraft-ttf v1.6](https://github.com/tryashtar/minecraft-ttf/releases/tag/v1.6) 发布的 `MinecraftDefault-Regular.ttf`；中文等字符保留原始 Unicode 字符，并使用同一发布包的 `MinecraftDefaultUniformBMP-Regular.ttf`，不做拼音化、转写或替换。字体随 Java renderer 打包，服务运行时不联网下载。

该字体在 **字号 12** 时精确复现原版 8px 字形单元（数字与大写字母宽 5、步进 6、空格 4），字形墨迹占基线上方 7px。渲染器据此把「字号 = 12 × GUI 缩放」「行顶到基线 = 7 × GUI 缩放」作为唯一文本度量：传入坐标即原版 `GuiGraphics.drawString` 的行顶坐标，容器标题 `(8, 6)`、物品栏标签、提示行等一律直接使用原版数值。所有 GUI 文本均以最近邻缩放渲染，缩放渲染（playerlist 2 倍、achievement 3 倍）的字体同步放大。

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
- **行生成顺序**（未显式给出 `title` 时，与 `ItemStack.getTooltipLines` 一致）：悬停名（`title` > `item.name` > `components["minecraft:custom_name"]` > `en_US` 语言文件 `item.minecraft.*`/`block.minecraft.*` > id 路径，颜色按 `components["minecraft:rarity"]`，自定义名斜体）→ `advanced` 时的物品 id（深灰）→ lore（`lines` 原样；否则 `item.lore` / `components["minecraft:lore"]`，缺省 `§5` 斜体）→ 附魔（`components["minecraft:enchantments"]`/`stored_enchantments` 的 `{levels:{id:等级}}` 或简化字段 `item.enchantments` 的 `"sharpness 5"`/`"sharpness V"`，灰色，等级 > 1 追加罗马数字）→ 属性修饰符（`components["minecraft:attribute_modifiers"].modifiers[]` 按 `slot` 分组，表头取 `item.modifiers.<slot>`，数值用 `#.##` 格式，蓝色）→ 单值组件（`unbreakable`、`dyed_color`，`advanced` 时的耐久度）→ `advanced` 时未识别组件的 `minecraft:<path>: <json>` dump 与 `item.components` 计数、`nbt` 存在时的 `item.nbt_tags` 计数。文案来自 `assets/minecraft/lang/en_us.json`。
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

手持物按原版 `ItemInHandLayer` 链路计算，但整条链在乘入场景空间之前仍处于原版实体模型空间：原版模型空间 y 向下、原点在颈部（脚底 +24px）、模型朝 -z，而玩家模型、披风与装备层所在的场景空间 y 向上、脚底为 0、模型朝观察者 +z，两者相差绕 x 轴半圈 `F = T(0, 24, 0)·Rx(180°)`。乘上 F 后，手持物与原版一致地挂在手心高度、位于躯干之前（默认 3/4 视角下主手与副手物品都可见），而不是落到躯干背后。显式传入 `options.width` / `options.height` 时输出整图最近邻缩放，语义与 `inventory` / `enderchest` 一致。

`playerlist` 使用原版 TAB 列表几何（9px 行距与 8×8 头像，本服务按 2 倍渲染以便在 Discord 等场景查看，字体同样放大 2 倍）：每个玩家必须提供 `texture`（与 `skin` 相同的校验，不能是网络 URL），头像取皮肤 `(8,8)` 脸部与 `(40,8)` 外层，右侧 ping 图标按原版阈值选择 `gui/sprites/icon/ping_5..ping_1`（`<150`/`<300`/`<600`/`<1000`），`ping <= 0` 或缺失时使用 `ping_unknown`；每列最多 20 行、多列从左到右、header/footer 每行居中。`gameMode` 字段仍接受并校验，但不会绘制，以保持与原版一致的行布局。

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

特殊 item definition 只实现 `shulker_box`、`chest`、`ender_chest` 三类 special 模型；其它 special 类型返回 `422 render_failed`，错误信息为 `unsupported special item model: <type>`，不会静默替换成普通图标。

## 健康与 fallback

`/healthz` 返回实际 backend 名称、版本、`real`、`fallback` 和支持 operations。真实 Java 命令不可执行时状态为 `degraded`；只有显式设置 `ALLOW_FALLBACK_RENDERER=true` 才允许 PrimitiveAdapter。fallback 仅用于测试/故障诊断，不代表 Minecraft 资源渲染结果。

常见错误：`409 resources_not_ready`、`422 render_failed`、`503 renderer_unavailable`、`400 invalid_data` / `invalid_image_options`。
