# 插件动态加载检查与启动失败自动诊断

## 背景

第三方插件可能在运行时导入当前 profile 不存在的依赖（例如玲珑版缺少 `@deepseek-ai/dsh-host-apiproxy`），导致 Cordis Loader 在 import 阶段直接崩溃，harness 完全无法启动。

现有 doctor 仅做静态检查（bundle 可解析、patch 可合成），无法发现这类运行时故障。用户遇到问题时只能看到白屏或反复重启，不知道原因也不知道怎么修。

## 目标

1. Doctor 能够检测出"第三方插件导致启动崩溃"类故障，并定位到具体 bundle
2. 启动连续失败时自动触发诊断，无需用户手动操作
3. 重试期间前端显示一致的加载状态，不跳回引导页

## 一、Doctor 动态加载检查（plugin-dynamic-load）

### 1.1 检查策略

采用「全量加载 + 二分定位」的两步策略：

1. **全量加载**：用 Cordis Loader 加载完整 profile（base + web-app + 用户 patch + 全部第三方 bundle），成功则检查通过
2. **二分定位**：全量加载失败时，通过二分法逐个排除第三方 bundle，找到导致崩溃的那个

选择理由：
- 大多数用户的插件都是正常的，单次全量检测最快
- 真出问题时，log₂N 次子进程即可定位，优于逐个加载
- 已有 `bisectThirdPartyBundles()` 工具可复用

### 1.2 子进程探测脚本

新增 `packages/support/doctor/src/loader-probe.ts`，作为独立可执行脚本。

**参数**：
- `--profile <name>` —— profile 名称（如 `web`）
- `--dsh-home <path>` —— harness home 路径
- `--include <bundle>` —— 可多次指定，只加载这些第三方 bundle（用于二分法）
- `--timeout <ms>` —— 超时时间，默认 10000ms

**退出码**：
- `0` —— 加载成功
- `1` —— 加载失败（stderr 输出错误堆栈）
- `2` —— 超时

**加载深度**：走完整的 Cordis Loader 合成流程（compose + load plugin tree），但不启动任何 HTTP 服务、不监听端口。加载完成（所有 plugin 的 `apply` 执行完毕）后立即 dispose，确保无副作用。

选择走完整 Loader 而非单纯 import 入口文件的原因：
- 能检测 cordis.yml 配置层面的故障（引用不存在的 service、plugin 导出格式错误等）
- 与静态检查的覆盖范围互补（静态查 patch 合成，动态查实际加载）
- 真正验证"这个 bundle 能不能和当前 profile 一起启动"

### 1.3 二分法实现

复用现有 `bisectThirdPartyBundles()` 工具框架，每次判定改为：
1. 构造子进程，传入当前候选 bundle 列表（通过 `--include`）
2. 等待子进程退出或超时
3. 退出码 0 → 这组没问题；非 0 → 这组有问题

候选列表来源：从用户的 `cordis.patch.yml` 中解析出的所有第三方 bundle。

如果多个 bundle 同时损坏，修复第一个后会再次触发检测，循环直到全部通过或耗尽候选。

### 1.4 检查注册

| 字段 | 值 |
|---|---|
| id | `plugin-dynamic-load` |
| name | 插件运行时兼容性 |
| category | `plugin` |
| severity | `fatal` |
| fixable | `true` |
| suggestedLevel | `2` |

### 1.5 修复逻辑（L2）

1. 定位导致崩溃的 bundle（全量探测 + 二分）
2. 备份 profile 的 `package.json`（字节级，`writeFileAtomic`）到 doctor 本次修复的 backup 目录
3. 用 `writeProfileManifest` 将 culprit 从 `dsh.profile.bundles` 移除 —— 与 `DSH_SAFE_MODE=plugins` 的排除模型一致（第三方 bundle 是 profile 层，不在用户 patch 文件里）
4. 重新运行动态加载检查验证
5. 验证通过 → 修复成功；仍失败 → 还原备份，报告失败原因

## 二、启动失败自动触发诊断

### 2.1 触发时机

`supervisor` 进入 `StateFailed` 状态（30 秒启动超时熔断）时，自动触发一次 doctor 诊断。

触发条件：
- 容器模式（非外置模式）
- 状态从非 failed 变为 failed
- 非用户手动停止
- 本次失败周期内尚未自动诊断过（避免重复触发）

选择 StateFailed 作为触发点的原因：
- 已经过指数退避重试，不是偶发故障
- 用户此时正对着启动失败界面，正好需要诊断结果
- 不会误触发（正常启动、手动停止均不触发）

### 2.2 现有重试机制

当前 supervisor 的重试参数：

| 参数 | 默认值 |
|---|---|
| 初始重启延迟 | 500ms |
| 最大重启延迟 | 10000ms |
| 启动超时（熔断） | 30000ms |

退避策略：指数退避 `500 × 2^(n-1)` ms，封顶 10s。累计启动失败超过 30s 进入 `StateFailed` 停止重试。

### 2.3 Go 端改动

在 `App` 结构体中增加：
- 启动失败诊断状态跟踪（避免重复触发）
- 状态事件中携带诊断运行状态或结果

状态事件 `FrontendStatus` 新增字段：
- `StartupDiagnosing bool` —— 是否正在进行启动失败自动诊断
- `StartupDoctorError string` —— 自动诊断的 doctor 命令错误（非空表示诊断本身失败了）

自动诊断在后台 goroutine 中运行，结果通过状态事件推送。前端可通过状态变化感知。

### 2.4 前端改动

当检测到 `State === "failed"` 且自动诊断完成时：
1. 自动弹出 doctor 诊断窗口
2. 顶部显示提示条："检测到启动失败，已为你自动诊断"
3. 如果 `plugin-dynamic-load` 检查失败，高亮显示并突出"中度修复（L2）"按钮

## 三、启动中 UI 优化

### 3.1 主舞台状态映射

| 状态 | 主舞台显示 |
|---|---|
| 外部已连接 | iframe（外部 URL） |
| 容器 running | iframe（容器 URL） |
| 容器 starting | 启动加载页 |
| 容器 failed | 启动失败页 |
| 容器 stopped（手动停止） | 引导页 |

### 3.2 启动加载页

居中布局，内容：
- 品牌标识
- "正在启动..." 文案
- Spinner 动画
- 底部可选提示："如果长时间无响应，请尝试安全模式"

### 3.3 启动失败页

居中布局，内容：
- 失败图标
- "启动失败" 标题
- 错误信息摘要（LastExit）
- 两个主操作按钮：
  - **诊断问题** —— 打开 doctor 弹窗
  - **以安全模式启动** —— 直接调用 StartSafeMode
- 底部小字提示：`~/.cache/dsh-desktop/harness.log` 查看完整日志

### 3.4 重试期间状态防抖

当前 supervisor 在重试延迟期间，进程已退出、状态为 `stopped`，延迟结束后才变回 `starting`。这会导致重试期间主舞台在引导页和加载页之间闪烁。

解决方案：**前端 1 秒 debounce**。

- 状态从 `starting` 变为 `stopped` 时，不立即渲染引导页
- 等待 1 秒；如果 1 秒内状态变回 `starting`，则不切换
- 如果 1 秒后仍为 `stopped`，再渲染 stopped 对应的界面

选择前端防抖的原因：
- Go 端状态机不用改，保持语义清晰（stopped 就是进程已停止）
- 实现简单，只改前端一处
- 不影响其他依赖状态的逻辑

## 四、边界情况

| 场景 | 处理方式 |
|---|---|
| 没有第三方 bundle | 检查直接通过 |
| 多个 bundle 同时损坏 | 逐个定位并修复，循环直到通过 |
| 子进程加载超时 | 视为失败，错误信息标记为"加载超时" |
| 玲珑沙箱环境 | 子进程通过正常 spawn 启动，沙箱内 node 可用 |
| 用户手动停止 | 不触发自动诊断（manuallyStopped 标记） |
| 外置模式 | 不触发自动诊断 |
| doctor 命令本身执行失败 | 前端显示"诊断失败"，不自动弹窗 |

## 五、改动文件清单

### doctor 包
- `packages/support/doctor/src/checks/plugins.ts` —— 新增 plugin-dynamic-load 检查
- `packages/support/doctor/src/loader-probe.ts` —— 新增子进程探测脚本
- `packages/support/doctor/src/bisect.ts` —— 适配/扩展二分法（按需）
- `packages/support/doctor/tests/` —— 新增测试

### desktop-launcher（Go）
- `apps/desktop-launcher/internal/app/app.go` —— 启动失败自动触发 doctor，状态字段扩展
- `apps/desktop-launcher/internal/supervisor/supervisor.go` —— 可能需要状态事件微调

### desktop-launcher（前端）
- `apps/desktop-launcher/frontend/index.html` —— 新增启动加载页、启动失败页结构
- `apps/desktop-launcher/frontend/app.js` —— 状态渲染逻辑调整 + 自动弹窗 + debounce
- `apps/desktop-launcher/frontend/styles.css` —— 新增样式
