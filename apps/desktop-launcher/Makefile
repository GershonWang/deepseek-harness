# DeepSeek Harness desktop launcher（Wails 栈）构建入口。
# Linux 一律带 -tags webkit2_41（wails 选用 webkit2gtk-4.1）。

GO       ?= go
TAGS     := production webkit2_41
BIN      := dsh-desktop-launcher
PKG_PATH := github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version
VERSION  ?= dev

.PHONY: build test vet fmt clean

build:
	CGO_ENABLED=1 $(GO) build -tags "$(TAGS)" \
		-ldflags "-X $(PKG_PATH)=$(VERSION)" \
		-o $(BIN) .

# test 与 vet 都必须带 $(TAGS)：不带标签时 cgo 走 wails 的 !webkit2_41 分支去解析
# webkit2gtk-4.0，而该 pc 文件只由打包脚本（linglong/prepare-pkgconfig.sh）临时生成，
# 开发机上并不存在，于是这两个目标根本建不起来。
# 另外 $(TAGS) 一定要加引号：不加的话 make 会把两个词拆成两个参数，
# webkit2_41 会被 go 当成包路径而不是标签值。
test:
	CGO_ENABLED=1 $(GO) test -tags "$(TAGS)" ./...

vet:
	CGO_ENABLED=1 $(GO) vet -tags "$(TAGS)" ./...

fmt:
	gofmt -w .

clean:
	rm -f $(BIN)