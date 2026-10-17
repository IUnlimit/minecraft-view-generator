# Minecraft View Generator

把 Minecraft `1.21.11` 的物品图标、界面与 3D 玩家渲染成 PNG 的 HTTP 服务。

服务端是 Go（只依赖标准库），真实渲染由 `renderer-backend/` 里的 **Java 21 无头 CPU renderer** 完成：它直接读 Mojang 官方 client JAR 与资源包栈，按反编译客户端的几何与纹理布局逐像素重建，不联网取图、也不使用伪造的占位图。未准备资源时接口不会返回假图，会明确报错；单个模型/贴图缺失时按原版降级为缺失模型，而不是让整张图失败。

## 特性

- **十种渲染类型**：`item`、`tooltip`、`inventory`、`item_container`、`enderchest`、`playerInventory`、`skin`、`playerlist`、`map`、`achievement`，几何、配色与文本度量对齐原版客户端。
- **完整物品模型支持**：1.21.11 item definition、model parent、generated/handheld、3D elements、纹理层、`select`/`condition`/`range` dispatch、八类 `minecraft:special`（旗帜 / 床 / 头颅与玩家头 / 盾牌 / 潮涌核心 / 饰纹陶罐 / 铜傀儡雕像 / 三叉戟）与箱子族贴图；组件驱动的图案、染色、陶片与内联皮肤。
- **文本按原版解析**：`§` 与 `&` 颜色/修饰符前缀、去码后的宽度参与居中与折行、原版字形盒与阴影规则。
- **版本化资源管理**：资源层级为 vanilla → addon → 用户资源包，通过 HTTP 接口显式准备与查询状态；客户端 JAR、资源对象按 SHA-1 缓存在数据目录，重复准备不重复下载。
- **资源缺失也降级**：未知物品、缺失模型、未直译的 special 族、损坏贴图、单个槽位失败都只影响那一层/那个槽位，并在 stderr 里说明原因。
- **内嵌交互式文档**：`/docs`（Scalar）与 `/api/v1/openapi.json`，内置十种类型的可直接发送报文。

## 目录结构

```
cmd/server                Go 服务端入口（HTTP、资源管理、渲染编排）
internal/httpapi          HTTP 路由、OpenAPI 文档、鉴权与 CORS
internal/resources        原版资源与资源包栈的下载、校验、版本状态
internal/renderer         渲染后端适配（子进程 JSON 协议）与图像合成
renderer-backend/         Java 21 渲染后端（源码、字体、fixture、build.sh/test.sh）
docs/api.md               接口与各渲染类型的详细说明
data/                     运行时数据：原版资源、资源包栈、准备状态（不纳入版本控制）
Dockerfile                三阶段镜像：Go 构建 → JDK 编译渲染后端 → JRE 运行
docker-compose.yml        单机部署编排
```

## 快速开始（Docker Compose）

需要 Docker Engine 与 Compose v2（本次在 Docker 29.9 / Compose 5.6 上验证通过）。

```sh
docker compose up -d --build

curl http://localhost:4666/healthz
```

容器内固定监听 `4666`，宿主机端口默认也是 `4666`；端口冲突时在仓库根目录新建 `.env`（已被 `.gitignore` 忽略）覆盖：

```sh
HOST_PORT=14666
```

`/healthz` 始终返回 `200`，`status` 字段反映渲染后端是否就绪（`ok` / `degraded`）；`docker compose ps` 里显示的 `healthy` 即探针检查到 `status":"ok"`。

访问地址：`http://<宿主机IP>:<HOST_PORT>`，文档页 `http://<宿主机IP>:<HOST_PORT>/docs`。

## 准备资源（首次必须执行）

服务不会自动渲染未准备版本，先触发一次下载与准备（约 500 MB，落在 `./data`）：

```sh
curl -X PUT http://localhost:4666/api/v1/resources/1.21.11   # 触发准备（幂等，已 ready 时直接返回）
curl http://localhost:4666/api/v1/resources/1.21.11          # 查询状态：stage/progress/status
curl http://localhost:4666/api/v1/versions                   # 列出所有版本状态
```

准备完成后 `status` 为 `ready`，之后渲染请求才会产出真实图像。`./data` 里的对象缓存按 SHA-1 存放，重复准备或升级版本时已下载的客户端 JAR、资源对象不会重复下载。

## 渲染示例

```sh
# PNG 直出
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"item","output":"png","data":{"id":"minecraft:diamond_sword","count":1}}' \
  -o item.png

# base64 内联
curl -X POST http://localhost:4666/api/v1/render \
  -H 'Content-Type: application/json' \
  --data '{"version":"1.21.11","type":"inventory","output":"base64","data":{"title":"Chest","rows":3,"columns":9,"slots":[]}}'
```

请求统一使用 `version`、`type`、`output`、`data`，以及可选的 `options.width/height/transparent/background/scale/frame`。各类型的能力边界、字段与几何细节见 [`docs/api.md`](docs/api.md)；交互式文档见 `/docs`。

## 本地运行（不使用 Docker）

依赖：Go 1.27.2+、JDK 21。

```sh
make build                     # 编译 Go 服务端到 dist/
renderer-backend/build.sh      # 编译 Java 渲染后端（产物：renderer-backend/build/minecraft-renderer.jar）
./run.sh                       # 启动（默认 0.0.0.0:4666，缺失时自动构建）
```

`run.sh` 会把 `DATA_DIR` 与 `RENDERER_COMMAND` 固定为仓库内的绝对路径，因此可以从任意目录调用。

## 配置

全部通过环境变量（`internal/config/config.go`），下表为默认值；Compose 已在 `docker-compose.yml` 中显式设置 `LISTEN_ADDR`、`DATA_DIR`、`RENDERER_COMMAND`。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080`（`run.sh` 为 `0.0.0.0:4666`） | HTTP 监听地址 |
| `DATA_DIR` | `./data` | 资源、缓存与准备状态的根目录 |
| `RENDERER_COMMAND` | `./renderer-backend/run.sh` | 渲染后端进程；留空则后端不可用 |
| `RESOURCE_PACK_ROOT` | 空 | 用户资源包根目录，按名称排序覆盖 addon 与 vanilla |
| `API_KEY` | 空 | 非空时启用鉴权：除 `/healthz`、`/docs`、`/scalar`、`/api/v1/openapi.json` 外都需 `X-API-Key` 或 `Authorization: Bearer` |
| `REQUIRE_RESOURCES` | `true` | 未准备资源时拒绝渲染而非返回假图 |
| `ALLOW_FALLBACK_RENDERER` | `false` | 真实渲染后端不可用时是否退回内置简化绘制 |
| `ADDON_ENABLED` | `true` | 是否准备 addon 资源层（失败只记 warning） |
| `CORS_ALLOWED_ORIGINS` | `*` | 允许的跨域来源，逗号分隔 |
| `MAX_CONCURRENT_RENDERS` | `4` | 并发渲染数（1–128） |
| `MAX_BODY_BYTES` | `2097152` | 请求体上限（≤ 64 MiB） |
| `MAX_IMAGE_WIDTH` / `MAX_IMAGE_HEIGHT` | `1600` / `1200` | 输出尺寸上限（≤ 4096） |
| `MAX_RENDER_DURATION` / `HTTP_TIMEOUT` | `15s` / `30s` | 单次渲染与 HTTP 超时 |
| `RENDERER_MAX_OUTPUT_BYTES` | `67108864` | 渲染后端输出上限 |
| `MAX_DOWNLOAD_BYTES` | `536870912` | 单个下载对象上限 |
| `MAX_ARCHIVE_FILES` / `MAX_ARCHIVE_BYTES` | `100000` / `2147483648` | 归档解包上限 |
| `MOJANG_MANIFEST_URL` | 官方 version_manifest_v2 | 版本清单地址 |
| `ADDON_VERSIONS_URL` / `ADDON_RESOURCE_URL` | loohpjames 插件接口 | addon 元数据与资源地址 |
| `ALLOWED_RESOURCE_HOSTS` | `piston-meta.mojang.com`、`piston-data.mojang.com`、`resources.download.minecraft.net`、`api.loohpjames.com` | 允许下载的上游主机白名单 |

## 测试

```sh
make test                                        # Go：go test ./...
make vet
cd renderer-backend && ./build.sh && ./test.sh    # Java：编译后跑 renderer smoke test
```

Java 侧 smoke test 用 `renderer-backend/fixture` 里的合成资源逐类型校验几何、颜色分层与降级路径，不依赖真实原版资源。

## 注意事项

- **数据目录不可随意搬家**：`data/states/<version>.json` 里记录的是资源根的**绝对路径**，换挂载路径或迁移目录后需要重新 `PUT /api/v1/resources/<version>`，否则渲染会按缺失资源降级。
- **容器以 uid 1000 运行**（镜像自带的 `ubuntu` 用户）。若 `./data` 属主不是本机 uid 1000，先 `sudo chown -R 1000:1000 data`，或在 compose 里加 `user: "0:0"` 用 root 运行。
- **每渲染一次拉起一个 JVM**：渲染后端是无状态命令行进程，每次请求 fork 一次 JVM（约百毫秒级），并发受 `MAX_CONCURRENT_RENDERS` 约束。
- **`/docs` 页面需要外网**：Scalar 前端从 jsDelivr CDN 加载，离线环境下直接看 `/api/v1/openapi.json`。
