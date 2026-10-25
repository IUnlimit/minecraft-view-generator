# AGENTS.md

把 Minecraft `1.21.11` 的物品图标、界面与 3D 玩家渲染成 PNG 的 HTTP 服务：Go 只依赖标准库做编排，真实渲染由 `renderer-backend/` 的 Java 21 无头 CPU renderer 完成（读 Mojang client JAR 与资源包栈，不联网取图）。

## 目录职责

| 路径 | 职责 |
| --- | --- |
| `cmd/server` | 进程入口：配置、HTTP 栈、资源管理与渲染管线的装配 |
| `internal/model` | **接口契约的唯一定义**：`Request` / `Options` / 各 type 的 data 结构体、json tag、校验与默认值 |
| `internal/httpapi` | 路由、鉴权、CORS、OpenAPI 文档（`openapi.go`）与示例目录 |
| `internal/renderer` | 渲染后端适配（子进程 JSON 协议）、画布合成、放大、简化后端 |
| `internal/resources` | 原版资源下载与校验、材质包栈（`data/resourcepacks` 扫描/解包/优先级/overlays/监听）与版本状态 |
| `internal/custom` | 自定义物品索引：ItemsAdder 配置子集、`items_ids_cache.yml`、模型扁平化与贴图内联 |
| `internal/font` | 原版字体 provider 管线（space / bitmap / unihex / reference） |
| `renderer-backend` | Java 渲染器：`src/main` 渲染代码、`src/test` 冒烟测试、`fixture` 自制资源包 |
| `docs/api.md` | 面向调用方的接口文档，含「请求参数总表」（`<!-- params:* -->` 标记段） |

## 构建 / 测试 / 运行

```sh
make build                  # 编译 Go 服务端到 dist/（OpenAPI 文档同时被内嵌进二进制）
make test                   # go test ./...
make vet                    # go vet ./...
renderer-backend/build.sh   # 编译 Java 渲染器 → renderer-backend/build/minecraft-renderer.jar
renderer-backend/test.sh    # 跑 RendererSmokeTest（只依赖 JDK 与 fixture，不下载资源）
./run.sh                    # 启动服务，默认 0.0.0.0:4666；源码比产物新时自动重建
```

本仓库用 **`javac` + `make`**，**没有 Gradle 工程**：不存在 `build.gradle`、`gradlew`，因此也没有 `./gradlew --stop` 这一步。Java 侧只有 `renderer-backend/build.sh` 与 `test.sh` 两个入口。

## 接口即契约：改参数必须同步文档

`/api/v1/render` 的报文就是产品契约。任何改变报文的行为都要在**同一个提交**里把文档补齐，不允许「先改代码，文档回头再说」：

1. **改 `internal/model` 里任何 json 字段（增、删、改名、改默认值、改校验、改渲染语义）**，必须同时更新：
   - `docs/api.md` 的 `<!-- params:start -->` … `<!-- params:end -->` 之间的**请求参数总表**；
   - `internal/httpapi/openapi.go` 的 `components.schemas`（`dataSchemas()` 与 `renderRequestSchema()`、`optionsSchema()`）；
   - 受影响的散文段落（类型支持矩阵、`special 物品模型`、`playerInventory 与 playerlist 说明`、`map 示例` 等）——同一事实只能有一处权威表述，其余位置指回参数总表。
2. **新增能力必须配可直接发送、能真正渲染成功的示例**：在 `renderExamples()` 里加一条，`output` 统一为 `base64`，皮肤/像素等大对象用现有的 `{{skinTexture}}` / `{{mapPixels}}` 占位符（`renderExamplesDocument()` 会在生成文档时替换成真实数据）。示例必须是完整报文，不能是片段。
3. **提交前必须 `go test ./...`**，它会机械校验两件事：
   - `internal/model/docs_test.go` 反射遍历 `Request`、`Options`、`Item`、`Slot`、`ContainerData`、`TooltipData`、`SkinData`、`PlayerInventoryData`、`PlayerListEntry`、`PlayerListData`、`MapMarker`、`MapData`、`AchievementData` 的每个 json tag，以及 `SupportedTypes()` 的每个 type，断言它们都在参数总表里以 `` `字段名` `` 的形式出现；漏写就失败并打印「参数总表缺少 `xxx`」。
   - `internal/httpapi` 校验示例的数量与顺序（前 `len(SupportedTypes())` 条必须与 `SupportedTypes()` 完全一致，额外示例排在后面）、每条示例都能 decode + `Validate` + `DecodeData` + 真实渲染成功，以及示例里的皮肤是内联 64×64 PNG。
4. **`make build` 会把 OpenAPI 文档内嵌进二进制**：改完文档要重新构建并重启，再抽查 `/api/v1/openapi.json`（`components.schemas` 是否可导航、示例是否齐全）与 `/docs`（Scalar 能否打开）。
5. **删除字段属于破坏性变更，先评估再动手**，并把兼容性结论写进提交信息：
   - 顶层字段与 `options` 内的未知键由 `DisallowUnknownFields` 拒绝 → 老客户端直接收到 `400 invalid_json`；
   - `data` 内的未知字段被**静默忽略** → 老客户端不会报错，但效果消失。
   正因为两类字段的兼容性完全不同，删除前必须确认它到底属于哪一类。
6. **改 Java 渲染行为**同样要更新行为描述（`docs/api.md` 的对应段落 + `openapi.go` 的示例 `description`），并在 `renderer-backend/src/test/.../RendererSmokeTest.java` 补断言：smoke test 是唯一能在没有真实资源的机器上验证几何与降级的关口。

## 语言与风格

- **文档用中文，代码注释用英文**。
- 提交信息用英文 conventional commits（`feat(renderer): …`、`fix(api): …`、`docs: …`）。
- **Go 只用标准库**，不引入第三方依赖（`go.mod` 里没有 `require`）。
- `gofmt` 是硬要求：`gofmt -l cmd internal` 必须为空。
