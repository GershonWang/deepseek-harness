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

test:
	CGO_ENABLED=1 $(GO) test ./...

vet:
	CGO_ENABLED=1 $(GO) vet -tags $(TAGS) ./...

fmt:
	gofmt -w .

clean:
	rm -f $(BIN)