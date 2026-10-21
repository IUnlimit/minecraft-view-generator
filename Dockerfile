# Minecraft View Generator — 镜像定义
#
# 三个阶段：Go 服务端 → Java 渲染后端（需要 JDK 编译）→ 运行时（只需要 JRE）。
# 渲染后端是一个独立的 jar，由 Go 服务端按请求以子进程方式拉起。

# ---- 1. Go 服务端 ---------------------------------------------------------
FROM golang:1.27.2 AS server-build

WORKDIR /src
# 本模块只依赖标准库，go.mod 里没有任何 require，因此这一步不需要网络。
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

ARG VERSION=dev
ARG COMMIT=unknown
# 与 Makefile 使用同一套 -X 注入方式，版本号会出现在启动日志与 /healthz 的响应里。
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o /out/minecraft-view-generator2 ./cmd/server

# ---- 2. Java 渲染后端 -----------------------------------------------------
FROM eclipse-temurin:21-jdk AS renderer-build

WORKDIR /src
# build.sh 用 javac + jar 产出 minecraft-renderer.jar；文本不再内嵌字体，字体资产由服务端 prepare 阶段物化到 data/。
COPY renderer-backend ./renderer-backend
RUN ./renderer-backend/build.sh

# ---- 3. 运行时 ------------------------------------------------------------
FROM eclipse-temurin:21-jre

LABEL org.opencontainers.image.title="minecraft-view-generator" \
      org.opencontainers.image.description="Minecraft 1.21.11 item and screen rendering HTTP service" \
      org.opencontainers.image.source="https://github.com/IUnlimit/minecraft-view-generator"

WORKDIR /app

# 渲染后端：只带 run.sh 与已编译好的 jar；字体读的是 data/ 里由 asset index 物化出来的 vanilla 资源树，不需要 fixture。
COPY --from=renderer-build /src/renderer-backend/run.sh /app/renderer-backend/run.sh
COPY --from=renderer-build /src/renderer-backend/build/minecraft-renderer.jar /app/renderer-backend/build/minecraft-renderer.jar
COPY --from=server-build /out/minecraft-view-generator2 /app/dist/minecraft-view-generator2

# 镜像自带的 uid 1000（ubuntu）与 Linux 上的普通用户一致，绑定挂载 ./data 时不会出现属主错配。
RUN mkdir -p /app/data && chown -R ubuntu:ubuntu /app

ENV LISTEN_ADDR=:4666 \
    DATA_DIR=/app/data \
    RENDERER_COMMAND=/app/renderer-backend/run.sh \
    TZ=Asia/Shanghai

EXPOSE 4666

USER ubuntu

ENTRYPOINT ["/app/dist/minecraft-view-generator2"]
