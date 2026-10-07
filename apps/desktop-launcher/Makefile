# DeepSeek Harness desktop launcher（Wails 栈）构建入口。
# Linux 一律带 -tags webkit2_41（wails 选用 webkit2gtk-4.1）。

GO       ?= go
TAGS     := production webkit2_41
BIN      := dsh-desktop-launcher
PKG_PATH := github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging.Version
VERSION  ?= dev
# 仓库根：doctor 的 vitest 由仓库根的 pnpm install 提供（doctor 自身不装 vitest）。
REPO_ROOT := $(abspath ../..)

.PHONY: build test vet fmt check clean

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

# check 是本目录全部可离线执行套件的唯一入口（AUDIT N-extra2）。
#
# 为什么需要它：这些套件此前各自为政——`test` 只跑 go test（且需 cgo 与系统 GTK 开发库，
# 普通开发机上建不起来），壳前端用例、doctor 用例与打包脚本自测只能靠人记得手动跑。
#
# 与 `test` 的分工：`test` 是需要 cgo/GTK 的完整 Go 套件；`check` 用 CGO_ENABLED=0 跑
# 不依赖 GTK 的内部包，从而在任何机器上都能执行。两者都不覆盖 `main` 包（需 cgo）。
#
# 只使用本目录内的文件与已安装的工具：`go`、`node` 在 PATH 上，doctor 的 vitest 由仓库根
# 的 pnpm install 提供。本目标不修改 apps/desktop-launcher/ 之外的任何文件。
# `preview.mjs verify` 需要 Chromium 且已在 pre-push 里，不进本目标。
check:
	@echo "==> Go 单元测试（CGO_ENABLED=0，不含需 cgo 的 main 包）"
	CGO_ENABLED=0 $(GO) test ./internal/... -count=1
	@echo "==> 壳前端用例"
	node --test frontend/test-app.cjs
	node --test frontend/test-i18n.cjs
	@echo "==> doctor 单元测试"
	@test -x "$(REPO_ROOT)/node_modules/.bin/vitest" \
		|| { echo "缺少 $(REPO_ROOT)/node_modules/.bin/vitest，请先在仓库根执行 pnpm install" >&2; exit 1; }
	cd doctor && "$(REPO_ROOT)/node_modules/.bin/vitest" run
	@echo "==> 打包脚本自测"
	@for t in linglong/test-*.sh; do echo "-- $$t"; sh "$$t"; done
	node linglong/test-link-bridge.cjs

clean:
	rm -f $(BIN)