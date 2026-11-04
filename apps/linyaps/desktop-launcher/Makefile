# DeepSeek Harness desktop launcher（Wails 栈）构建入口。
# Wails v3 默认使用 GTK4 + WebKitGTK 6.0，不再需要 webkit2_41 之类的标签；
# production 标签决定 /wails/runtime.js 提供压缩版运行时。

GO       ?= go
TAGS     := production
BIN      := dsh-desktop-launcher
# 仓库根：doctor 的 vitest 由仓库根的 pnpm install 提供（doctor 自身不装 vitest）。
REPO_ROOT := $(abspath ../../..)

.PHONY: build test vet fmt check clean

build:
	CGO_ENABLED=1 $(GO) build -tags "$(TAGS)" -o $(BIN) .

# test 与 vet 都带 $(TAGS)：cgo 需要链接 GTK4/WebKitGTK 6.0 的开发包
# （libgtk-4-dev、libwebkitgtk-6.0-dev），否则这两个目标建不起来。
# $(TAGS) 保持加引号：将来标签变多时，不加引号会被 make 拆成多个参数。
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
# 的 pnpm install 提供。本目标不修改 apps/linyaps/desktop-launcher/ 之外的任何文件。
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