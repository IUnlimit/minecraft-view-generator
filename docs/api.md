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
curl http://localhost:4666/api/v1/versions
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

`output=png` 返回 `image/png`；`output=base64` 返回 `{ "mimeType":"image/png", "encoding":"base64", "data":"...", "width":n, "height":n }`。响应里的 `width`/`height` 就是这张图的最终尺寸。

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

各类型的原生画布：`item` 正方形（缺省 128×128）、`tooltip` 宽 360 高由内容决定、`inventory` / `enderchest` / `item_container` 取各自界面贴图尺寸（普通容器 176×166，盒子按行数 `176×(17 + 18×行数 + 96)`，hopper 176×133）、`skin` 256×384、`playerInventory` 176×166、`playerlist` 宽度按列数（最少 360）高由人数决定、`map` 140×140、`achievement` 480×96。原生高度由内容决定的类型（`tooltip`、`playerlist`）只接受画布宽度，高度仍交给渲染器自算；需要限制最终高度时用 `scale` 的目标框。

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
| `item` | 1.21.11 item definition、model parent、generated/handheld、3D elements、纹理层、select/condition/range dispatch、九类 `minecraft:special` 几何（旗帜 / 床 / 潜影盒 / 头颅与玩家头 / 盾牌 / 潮涌核心 / 饰纹陶罐 / 铜傀儡雕像 / 三叉戟）与箱子族平面贴图、图案与染色组件、damage/glint、资源缺失时画原版缺失模型 |
| `tooltip` | 原版九宫格背景（`gui/sprites/tooltip/background`，border 从 `.mcmeta` 读取）与 advanced 边框（`tooltip/frame`）、`§` 与 `&` 颜色/修饰符前缀、原版行生成顺序（悬停名 → advanced id → lore → 附魔 → 属性修饰符 → 单值组件 → 组件/NBT 计数，lore 只来自 `item.lore`）、按真实文本宽度折行、`icon` 开关 |
| `inventory` | 官方 `generic_54` chest GUI，1–6 行九列、标题、主容器槽、玩家区槽位、堆叠数量 |
| `item_container` | hopper（176×133）、furnace/blast_furnace/smoker、crafting_table、anvil、brewing_stand（176×166）；槽位取自各自原版菜单；未知容器返回 `unsupported_container` |
| `enderchest` | 固定 3×9，与普通三行箱子完全一致（同一 `generic_54` 贴图、配色与槽位，无额外叠加） |
| `playerInventory` | 原版 InventoryScreen 1:1 布局（默认 176×166，面板贴图不缩放）、标题 `(97, 6)`、原版预览视口内的 3D 玩家（26→75 / 8→78，30 像素每方块）、外层与披风、`equipment/*.json` 盔甲分层（染色层、elytra 翼）、手持物 thirdperson 3D 变换（手部高度、躯干前方，special 模型也用 3D 几何）、InventoryMenu 槽位（背包 / 快捷栏 / 装备 / 副手） |
| `skin` | 原版 3D 玩家模型：64×64 classic/slim、legacy 64×32（自动补齐左侧肢体）、外层第二层、原版斗篷 UV、手持物 thirdperson 3D 变换（含在自适应缩放内；special 模型同样使用其 3D 几何） |
| `playerlist` | 原版 TAB 布局：行距 9px、8×8 头像、官方 ping 图标与阈值、header/footer 居中、多列（每列 20 行），最多 500 人，整体 2 倍渲染（字体同步放大）；`gameMode` 接受校验但不绘制 |
| `map` | 原版地图图像：`map/map_background` 羊皮纸背景 + 内缩的完整 128×128 像素网格（默认 140×140，内容 1:1）+ `map/decorations/*` 原版装饰图标，最多 16384 项 pixels 或解码后最多 16384 字节 pixelData、marker rotation/type/name、dimension/locked/scale |
| `achievement` | 官方 advancement toast（160×32 内容，默认放大 3 倍到 480×96）、task/goal/challenge obtained/unobtained frame、icon、标题与描述在 x=30 的第 7/18 行 |

## 字体

文本完全走原版 `FontManager` 的 provider 管线：`assets/minecraft/font/default.json` 依次声明 `space` → `bitmap` → `reference`（`include/unifont`）→ `unihex`，Java renderer 与 Go 简化后端（`internal/font`）解析同一份资源、执行同一套规则。渲染器不再内嵌任何 TTF，服务运行时不联网下载字体。

| provider | 资产 | 行为 |
| --- | --- | --- |
| `space` | `default.json` 内联 | 空格步进 4px，不产生像素 |
| `bitmap` | `textures/font/ascii.png`（ascent 7）、`accented.png`（ascent 10、高 12）、`nonlatin_european.png` | 拉丁与重音字形；保留左空距，只裁掉右侧空列，步进 = 墨迹宽 + 1 |
| `reference` | `font/include/unifont.json` | 内联 Unifont 子定义（含 `size_overrides`），可继续嵌套 |
| `unihex` | `font/unifont.zip` | 16×16 Unifont 点阵按 2×2 覆盖度降到 8×8，每个像素保留 0/1/2/3/4 五档覆盖度（半透明灰阶）；`size_overrides` 把 CJK 块固定为全角，步进 9 |

`filter`（`uniform` / `jp`）按原版语义求值：默认字体不启用任何选项，因此 `uniform: false` 的 bitmap 组保留、`jp: true` 的日文 Unifont 变体被跳过。中文保留原始 Unicode 码点，不做拼音化、转写或替换，字形来自 `unifont.zip` 的点阵而非矢量字体。

度量与原版一致：传入坐标就是 `GuiGraphics.drawString` 的行顶坐标（基线 = 行顶 + 7），空格 4、普通字形「墨迹宽 + 1」、CJK 全角 9，tooltip 行高 10。`bitmap` 贴图是绘制好的位图，像素只有「有墨 / 无墨」两态；`unihex` 的 16×16 点阵同时保留原始网格与 2×2 归并后的 8×8 视图：1 倍时用 8×8 的 5 档覆盖度（`0`–`4` 四分之一）绘制，1 档与 2 档半透明，就是原版 GUI scale 1 的柔边；**2 倍及以上改用原始 16×16 网格重采样**，2 倍时一个 texel 正好落在 1 个设备像素上（笔画是实色，不再是灰块），3 倍等非整数倍按面积覆盖做一次正确的抗锯齿。上述字体资产由 `prepare` 阶段从官方 asset index 物化到 `data/resources/<version>/vanilla/assets/minecraft/font/`，与其它原版资源一同按 SHA-1 缓存，并在资源根目录写一份 `font-overlay.json` 记录本次物化的文件清单。

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

## 颜色码（`§` 与 `&`）

所有文本入口（容器标题、`playerInventory` 标题、playerlist 的 header/footer 与玩家名、`map` 的 dimension 与标记名、`achievement` 的标题与描述、tooltip 的每一行）都按同一套原版 legacy 格式解析：`0-9a-f` 十六色（原版调色板）、`l` 粗体、`o` 斜体、`n` 下划线、`m` 删除线、`k` 混淆（确定性渲染，不做随机化）、`r` 重置。原版只认 `§`（U+00A7）；`&` 是本服务的兼容扩展，两者都作为前缀接受，**只有后跟合法码字符时才算格式码**，因此字面量 `&` 与 `A& B` 原样保留（消费方不必再自行把 `&` 转成 `§`）。

未带码的文本使用该入口在游戏中用的颜色（容器标题 `0x404040`、tooltip/playerlist/achievement/map 为白色、`achievement` 完成态标题为 `0xFFFF55`），`§r` 也回到这个颜色。格式码不计入宽度：容器标题的居中、playerlist 的列宽与 header 居中、tooltip 的折行都用去码后的宽度。全部文本均带原版字形盒几何（行顶到底部 7px × 缩放），容器标题与地图标签按原版不带阴影，tooltip 与 playerlist 带阴影。

## special 物品模型

`minecraft:special` 的模型定义在客户端代码里而不是 model JSON 中，本服务按 1.21.11 反编译结果逐盒直译几何与贴图区域，语义与客户端一致（同一个 `texOffs` + `PartPose` 盒列表、同一套贴图展开、同一个 `display` 变换）：

| 族（模型定义 `model.type`） | 几何与贴图 |
| --- | --- |
| `banner` | 旗杆 + 横杆（`entity/banner_base`）与旗面：先按模型的 `color` 染 `entity/banner/base`，再按 `banner_patterns` 的每个图案染 `entity/banner/<pattern>`；`base_color` 组件覆盖模型颜色 |
| `bed` | 床头 / 床尾两半（`entity/bed/<texture>`），按 `BedRenderer` 的 `translate(0, 0.5625, 0/-1) → rotateX(90°)` 摆放 |
| `head` | `kind` 对应 `entity/{skeleton, creeper, zombie, piglin, enderdragon}/…`，`texture` 字段可覆盖；8³ 头颅 + 0.25 膨胀覆盖层（humanoid kind），固定 `yRot = 180°` 面对观察者 |
| `player_head` | 同上的人类头层，皮肤取 `profile` 组件里内联 PNG；没有内联皮肤（含只给远端 hash 的 profile）时回退到客户端默认皮肤 `entity/player/wide/steve` |
| `shield` | 盾面 + 握把（`entity/shield_base_nopattern` / `entity/shield_base`），有图案时按 `banner_patterns` 逐层染 `entity/shield/base` 与 `entity/shield/<pattern>` |
| `conduit` | 未激活状态的外壳（6³，`entity/conduit/base`）+ `translate(0.5, 0.5, 0.5)`；笼、风与眼睛三层属于方块实体的激活状态，不在物品图标里 |
| `decorated_pot` | 颈 + 盖 + 四面侧壁（`entity/decorated_pot/*`），`pot_decorations` 按原版 back/left/right/front 顺序选陶片图案 `*_pottery_pattern`，没有陶片的侧面用 `decorated_pot_side` |
| `copper_golem_statue` | 站立姿势的铜傀儡盒列表（`texture` 指向氧化程度贴图）；`sitting` / `running` / `star` 三种姿势尚未直译，暂用站立姿势 |
| `trident` | 杆 + 基座 + 三叉（`entity/trident`）；GUI 上下文的 `select(display_context)` 会选中平面模型 `item/trident`，因此这条几何用于手持 |
| `shulker_box` | 关闭状态的潜影盒：盒盖 `lid`（`texOffs(0, 0)`，16×12×16）与盒身 `base`（`texOffs(0, 28)`，16×8×16）两个盒子叠出 1×1×1 方块，`texture` 字段直接给出染色后的贴图（`entity/shulker/shulker` 为未染色，`entity/shulker/shulker_red` 等为 16 色），`display.gui` 的 30°/45° 与 0.625 让它在图标里呈等轴 3D 方块；盒盖底边 4 单位透明、盒身顶边透出的 8 像素凸起就是原版贴图的卡扣 |
| `chest` / `ender_chest` | 仍是原版平面实体贴图（`entity/chest/normal`、`entity/chest/ender`），与之前版本逐像素一致 |

图标与手持共用同一份几何：图标走「`display.gui` 的 `T·R(rotationXYZ)·S·T(-0.5,-0.5,-0.5)` + 正交直视相机（1 单位 = 1 像素、物品中心落在图标中心）」，手持走 `thirdperson_righthand` / `thirdperson_lefthand` 与 `ItemInHandLayer` 链条，深度排序与遮挡和玩家模型共用。

## 自定义物品（`customData`）

原版物品的图标由 item definition 决定，而 ItemsAdder 这类插件用的是「同一个基础材料 + 一个 `custom_model_data` 数值」来表达一件自定义物品。本服务直接把这条映射读到渲染链路里：请求的物品带上 `customData`，就会按用户包里的贴图与模型几何绘制，而不是画原版材料。

```sh
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","data":{"id":"minecraft:paper","customData":1134}}' \
  -o starfish.png
```

- **定位键 = 基础材料 + 数值**：`id` 的命名空间与 `customData` 组合成索引键（`minecraft:paper` + `1134` → `PAPER#1134`），与 `items_ids_cache.yml` 里的 `<MATERIAL>: {<namespace>:<name>: <cmd>}` 对应。同一数值在不同材料下是不同物品（`PAPER 1134` 与 `FISHING_ROD 1134` 互不相干），所以调用方必须传真实的基础材料。
- **`customData` 与 `customModelData` 是两件事**：`customModelData` 仍然喂给原版的 `range_dispatch` 覆盖；`customData` 只用于自定义内容索引。
- **任何物品位置都生效**：`item`、`tooltip.item`、`inventory/item_container/enderchest.slots[].item`、`playerInventory.armor/mainHand/offhand`、`achievement.icon`。
- **绘制的是贴图 + 模型几何**：模型 JSON 的 `parent` 链会被逐层合并（遇到 `minecraft:` 父模型即停，并保留它以便沿用原版的 `display` 手部变换），`textures` 里的自定义贴图按需读文件并内联成 data URI 传给渲染后端。因此 `generated` 平面图标、带 `elements` 的 Blockbench 家具模型、以及**手持物品**都能画。
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
  "versions": [{
    "version": "1.21.11",
    "status": "ready",
    "resourceRoot": "/app/data/resources/1.21.11",
    "vanillaRoot": "/app/data/resources/1.21.11/vanilla",
    "addonRoot": "/app/data/resources/1.21.11/addon",
    "packRoots": ["/app/data/packs/pack-000"],
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
