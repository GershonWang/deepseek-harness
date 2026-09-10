# Agent Note: Desktop launcher preflight gate and fresh-home fallback

Status: implemented

English | [中文](2026-09-10-desktop-launcher-preflight-and-fresh-home.md)

## Problem

桌面启动器（`apps/desktop-launcher`）启动 harness 后仅在**事后**才诊断：supervisor 监听确定性加载失败特征进入 `StateFailed`，再触发后台 doctor。在此之前用户会看到数次"启动几秒后停止"的循环。开发阶段第三方插件不兼容是常态，这套事后流程虽然能修复问题本身，但无法参与启动决策；且没有最后手段的降级启动路径——当用户数据目录本身存在 doctor 无法修复的损坏时（profile `node_modules` 半安装状态、`storages/*.json` 损坏、凭证不可读），harness 既无法正常启动，也无法进入任何可用状态。

## Decision

桌面启动器在 harness 首次启动前插入**启动前预检**（preflight gate），并在预检穷尽时提供**全新环境降级启动**路径：

- **门控**（`internal/supervisor`）：`Config` 增加 `Env`（per-spawn 子进程环境，nil 继承）；supervisor 增加 `Gate()`/`Release()`——`Gate()` 后 run 循环在首次 spawn 前等待 startCh token，使 `New()` 可以把首次启动挂到预检决策完成。`SetEnv` 在各次 spawn 间替换子进程环境，不影响正在运行的进程。
- **预检编排**（新增 `internal/preflight` 包 + `internal/app/preflight.go`）：app gate 在首次 spawn 前运行 `dsh doctor --json --quick`（仅静态检查），剥离 `DSH_SAFE_MODE` 并将 `DSH_HOME` 固定指向真实 `~/.dsh`。报告无 fatal 直接放行；有 fatal 时先自动执行 level-1 修复（低风险可逆：注释 `.env` 违规行）并快速复查；仍有 fatal 则挂到 `needs-confirm` 阶段，问题清单渲染在专用舞台页面，由用户选择：深度修复（`--repair 2`，含真实 boot 探测 + 全量复查）、仍然启动（保留现有事后诊断兜底）、安全模式（`config` 级，跳过第三方 bundle 和用户补丁层）、或全新环境启动。
- **全新环境降级**（`StartFreshHome`）：后续 spawn 注入 `DSH_HOME=<home>/.dsh-fallback`，harness 在空目录自动自举完整的模板 profile 与默认设置。原始 `~/.dsh` 原样保留，永不写入或删除；fallback 目录持久化以保留降级期间的配置。凭证**不迁移**（含 API Key 等敏感信息），降级环境通过继承的环境变量或重新配置取得密钥。
- **时序约束**：快速诊断限时 15s，修复 60s，全量复查 3 分钟；doctor 子进程均登记到现有 doctor 追踪（shutdown 可取消）。doctor 本身失败或超时**绝不阻塞启动**，门控直接放行，事后诊断兜底。预检定位为尽力而为的前置检查。
- `RunDoctor`/`RunDoctorRepair`（doctor 面板）改用同一 `preflight.Runner` 提供 argv 和子进程环境，strip-`DSH_SAFE_MODE`/pin-`DSH_HOME` 规则收敛为单一来源。

## Alternatives considered

**每次启动都跑全量 doctor（含真实 boot 探测）。** boot 探测在子进程中 boot 完整插件树，最长 60s，每次正常启动都付这个代价不可接受。quick 模式（`--quick`）仅跳过探测，亚秒级完成；探测只在可疑结果或用户确认的深度修复时才消耗。

**全自动降级（先安全模式，再不行直接全新环境）。** 修复第三方插件不兼容通常涉及移除插件和重置 settings.yaml；全新环境使会话、设置、凭证全部不可用。两者都有用户可见的后果且不经过确认，只有 level-1 自动修复例外。

**凭证复制进 fallback 目录。** 扩大泄露面，违背安全原则。

**用 `DSH_SAFE_MODE=plugins` 作为降级建议，不新增 config 级安全模式 UI。** `plugins` 保留用户补丁层——而用户补丁层本身就是常见的启动失败来源（补丁 target 在版本间被重命名）；预检已知残余失败是否在 config 层，因此直接提供 `config` 级，原有的 failed 页面按钮保留 `plugins` 级。

## Consequences

损坏的用户数据目录不再能阻塞启动器：门控超时进入 `error` 阶段并放行，全新环境路径完全绕过用户层且不触及原始数据。代价是正常路径多了一次快速 doctor 子进程调用（实际亚秒级，上限 15s），以及启动器表面增大：新增舞台页面、四个绑定方法、supervisor 的 `Env`/门控语义。fallback 目录在多次降级间累积状态，不自动清理，用户需自行迁移数据回 `~/.dsh`。

## Testing

`go test`（`apps/desktop-launcher`）覆盖：门控语义与环境注入（`internal/supervisor/gated_env_test.go`，用 canary-through-ready-URL mock）；doctor argv/env 组装、JSON 解析、严重/修复级别分桶（`internal/preflight/preflight_test.go`）；编排状态机（`internal/app/preflight_test.go`，mock doctor 脚本覆盖：健康放行、fatal-without-autofix 挂在 needs-confirm、doctor 失败不阻塞、fresh-home 环境注入清除安全模式）。`node --test frontend/test-app.cjs` 覆盖舞台切换、问题清单渲染与 Kind 徽标、操作按钮显示规则、服务器弹框中 fresh-home 标识。

## Related

本次新增的事前预检复用并前置了现有事后诊断机制：[Doctor live-load check for third-party plugins](2026-08-28-doctor-plugin-dynamic-load.zh.md)。
