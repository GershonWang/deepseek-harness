# Wails v3 spike 构建入口。
#
# 为什么 Linux 默认不带 gtk3：v3 的默认后端是 GTK4 + WebKitGTK 6.0，这正是升级要验证的目标栈；
# GTK3 只在 build-gtk3 里显式开启，作为 GTK4 受阻时的过渡对照，二者不可混用。
GO      ?= go
BIN     := bin/dsh-v3-spike
TAGS    ?= production
REPORT  ?= $(CURDIR)/bin/report.json

.PHONY: check tools build build-gtk3 run verify clean

# check 在任何机器上都能跑：用 Windows 目标交叉编译做纯 Go 的类型检查，不链接 GTK。
# 它覆盖的是「API 用法是否正确」，覆盖不了「GTK4 能不能链上」——后者只能在真机做。
check:
	gofmt -l . | tee /dev/stderr | test -z "$$(cat)"
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build ./...
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) vet ./...

# tools 检查真机构建前提：两个 pkg-config 包缺一不可（GTK4 后端会同时链接 WebKitGTK 6.0）。
tools:
	@command -v pkg-config >/dev/null || { echo "缺少 pkg-config：请安装 libgtk-4-dev 与 libwebkitgtk-6.0-dev" >&2; exit 1; }
	@pkg-config --exists gtk4 || { echo "缺少 gtk4 开发包（libgtk-4-dev）" >&2; exit 1; }
	@pkg-config --exists webkitgtk-6.0 || { echo "缺少 webkitgtk-6.0 开发包（libwebkitgtk-6.0-dev）" >&2; exit 1; }
	@pkg-config --modversion gtk4 webkitgtk-6.0

build: tools
	CGO_ENABLED=1 $(GO) build -tags "$(TAGS)" -o $(BIN) .

# build-gtk3 用于对照：wails v3.1.0 源码里 gtk3 后端仍在，但只在 deepin 25 base 的 GTK3 上可用。
build-gtk3:
	CGO_ENABLED=1 $(GO) build -tags "$(TAGS) gtk3" -o bin/dsh-v3-spike-gtk3 .

run: build
	DSH_V3_SPIKE_REPORT="$(REPORT)" ./$(BIN)

verify:
	sh ./verify.sh

clean:
	rm -rf bin
