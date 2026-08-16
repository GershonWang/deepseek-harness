# Agent Note: 桌面启动器的外部 harness 连接

Status: implemented

[English](2026-08-16-desktop-launcher-external-harness.md) | 中文

## Problem

`apps/desktop-launcher/` 的桌面启动器只加载它在玲珑容器内监管的 harness Web UI。用户也在宿主机(`npx @deepseek-ai/dsh web`)或网络可达的其他机器上运行 web 版,而启动器没有途径让 webview 指向这个外部服务而非容器内的那个。

## Decision

服务器状态弹框新增连接模式单选(容器内 / 本机或远端服务)、外部服务地址行(连接/断开按钮)与外部状态区。所有改动都留在 `apps/desktop-launcher/` 内;`supervisor.go` 的容器状态机原样保留。

连接外部服务按固定顺序执行:校验并规范化 URL(`Connector.ValidateURL`),非回环 host 每次会话确认一次(`NeedConfirmation`/`ConfirmHost`),先停容器 harness(`StopHarness` 释放端口并暂停自动重启),再在 goroutine 里做一次 HTTP 探测(`Connector.BeginExternal`,3s 超时)。探测成功把 `connector.Mode()` 切到 `ModeExternal` 并让 webview 导航到外部 URL;探测失败自动恢复容器流程——弹框内显示错误并调用 `Supervisor.Restart()` 把容器 harness 拉回来。断开执行反向流程:`EndExternal()`、重启容器、等就绪 URL(30s 上限)再导航回去。

`connector.Mode()` 是当前模式的唯一依据;弹框单选按钮永远镜像它,绝不反向。所有改变模式的路径都会重新同步单选按钮——打开弹框、点击连接、探测成功、探测失败、断开——且已连接时 `dshOnModeChanged` 强制单选回外部,用户无法在 webview 仍显示外部服务时切到容器内(从而误启用容器按钮)。

goroutine 结果经 idle 回调的 `gpointer` 回到 GTK 主线程:goroutine 用 `C.CBytes` 把探测结果(URL + 错误消息)或待导航 URL 写进 C 内存,C 侧 idle 回调把指针交给 Go 处理函数,函数返回后释放。不存在包级结果变量,因此交接不引入 Go 数据竞争;`Connector` 自身保持互斥锁保护、不做改动。

外部 URL 在连接成功后持久化到 `~/.config/dsh-desktop/config.json`(`{"externalUrl": "..."}`),打开弹框时预填;启动器从不自动重连。

## Alternatives considered

**互斥锁保护的包级全局变量。** 交接可以保留 `probeResultURL`/`probeResultErr`/`pendingNavURL` 并用 `sync.Mutex` 保护。gpointer 路线更干净:C 侧 idle 回调本来就有 `gpointer` 参数,载荷直接挂上去,不需要任何需要记得加锁的共享 Go 状态。

**在 GTK 主线程探测。** 阻塞式探测会冻结弹框最长 3s 超时。探测在 goroutine 里跑,经 `g_idle_add` 回报,UI 保持响应。

**探测成功后再停容器。** 被否决:探测期间容器内 harness 会占着端口并继续自动重启,可能和外部服务端口冲突。`StopHarness` 在探测前执行。

**记住 URL 并自动重连。** 被否决:持久化只用于预填弹框;重连永远是用户显式操作,自带新的探测与确认。

## Consequences

玲珑沙箱共享宿主网络命名空间——已实证:沙箱与宿主处于同一 netns,沙箱内可对宿主 loopback 拿到 HTTP 200——因此 webview 无需任何沙箱网络配置即可访问宿主 loopback 与宿主可达的任何地址。

远端 harness 必须绑定非回环接口才能被访问(`dsh web --host <LAN-IP>`);`--host 0.0.0.0` 被上游故意拒绝,远端浏览还需要 `--trusted-host`。安全确认弹框在连接前以纯文本展示目标 URL,拼写错误或过期的自动填充项在 API key 与命令发往那台机器之前对用户可见。

失败路径是全自动的:探测失败即重启容器 harness,用户不会被困在「外部模式 + 死 URL + 容器已停」的状态。容器 supervisor 自身的重启/停止语义原样复用。
