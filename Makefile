# Minecraft View Generator — 多平台构建脚本
# 用法:
#   make build            当前平台构建
#   make release          全部平台交叉编译
#   make build-linux-amd64  单平台构建
#   make test / vet / clean

BINARY   := minecraft-view-generator2
CMD      := ./cmd/server
DIST     := dist
VERSION  ?= $(shell date -u +v%Y%m%d)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

# 目标平台: OS-ARCH 组合
PLATFORMS := \
	linux-amd64 \
	linux-arm64 \
	darwin-amd64 \
	darwin-arm64 \
	windows-amd64 \
	windows-arm64 \
	freebsd-amd64

GO       ?= go
GOFLAGS  ?= -trimpath

.PHONY: all build run release test vet fmt clean $(PLATFORMS)

all: test build

## 当前平台构建
build:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) $(CMD)

## 本地运行
run: build
	$(DIST)/$(BINARY)

## 交叉编译全部平台
release: $(PLATFORMS)

# ---- 单平台目标: make linux-amd64 / make darwin-arm64 / ... ----
$(PLATFORMS):
	$(eval OS   := $(word 1,$(subst -, ,$@)))
	$(eval ARCH := $(word 2,$(subst -, ,$@)))
	$(eval EXT  := $(if $(filter windows,$(OS)),.exe))
	@mkdir -p $(DIST)
	GOOS=$(OS) GOARCH=$(ARCH) CGO_ENABLED=0 $(GO) build $(GOFLAGS) \
		-ldflags '$(LDFLAGS)' \
		-o $(DIST)/$(BINARY)_$(OS)_$(ARCH)$(EXT) $(CMD)
	@echo "=> $(DIST)/$(BINARY)_$(OS)_$(ARCH)$(EXT)"

## 全部测试
test:
	$(GO) test ./...

## 静态检查
vet:
	$(GO) vet ./...

## 代码格式化
fmt:
	gofmt -w cmd internal

## 打包 release 产物 (tar.gz / zip)
package: release
	cd $(DIST) && \
	for f in $(BINARY)_linux_* $(BINARY)_darwin_* $(BINARY)_freebsd_*; do \
		tar -czf $$f.tar.gz $$f; \
	done && \
	for f in $(BINARY)_windows_*; do \
		zip -q $$f.zip $$f; \
	done && \
	ls -lh

## 清理产物
clean:
	$(GO) clean
	-rm -rf $(DIST)
