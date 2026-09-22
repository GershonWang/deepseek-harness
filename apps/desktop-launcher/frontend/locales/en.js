/* 壳前端英文文案字典：与 zh.js **同键集**。
 *
 * 为什么与 zh.js 分开成文件：两份字典是同一批键的两种语言，语言之间不应互相
 * 引用；加载顺序（index.html 保证）只决定挂到全局的先后，不决定回退关系——
 * 回退关系在 i18n.js 的 lookup 里，缺键一律先回退 zh，再回退键名本身。
 *
 * 键集由 zh.js 决定，本文件不自增键：键只增在 zh.js，英文照抄键名改值，否则两处
 * 各自长键、总有一处漏。test-i18n.cjs 的「字典完整性」用例比对键集、占位符与
 * 「值与中文逐字相同」三种情况：最后一类只放过 status.exitCode 这种两种语言本来
 * 就同形的条目。
 *
 * 字符串里的 {name} 是占位符，与 zh.js 同键必须同占位符。句子中间的 `<code>` 由
 * HTML 提供，于是 .before / .after 两段要各自读通：.before 结尾、.after 开头会被
 * HTML 里的空格与命令串接起来，英文语序在这两段里自己调整，不要回到代码里拼词序。
 */
"use strict";

window.DSH_LOCALES = window.DSH_LOCALES || {};
window.DSH_LOCALES.en = {
  /* ---------- 通用 ---------- */
  "common.listSeparator": ", ",
  "common.close": "Close",

  /* ---------- 标题栏 ---------- */
  "titlebar.server": "Server",
  "titlebar.tools": "Toolchains",
  "titlebar.terminal": "Terminal",
  "titlebar.doctor": "Diagnosis & repair",
  "titlebar.about": "About",
  "titlebar.minimize": "Minimize",
  "titlebar.maximize": "Maximize",
  "titlebar.close": "Close",

  /* ---------- 引导页 ---------- */
  "guidance.intro": "No harness session is available: the in-container service is not running and no external service is connected. Pick one way to start (if a service is starting, give it a moment).",
  "guidance.container.title": "In container",
  "guidance.step.openServer": "Click “Server” in the top right",
  "guidance.container.keepMode": "Keep the “In container” mode",
  "guidance.container.start": "Click “Start” and wait until it is ready",
  "guidance.remote.title": "Connect to a local/remote service",
  "guidance.remote.switchMode": "Switch to “Local/remote service”",
  "guidance.remote.fillAddress": "Enter the service address and click “Connect”",
  // 连起来读：Start the target harness with `dsh web --host <LAN-IP>` to reach it
  // from another machine; the first connection to a non-local address needs confirmation.
  "guidance.remote.hint.before": "Start the target harness with",
  "guidance.remote.hint.after": "to reach it from another machine; the first connection to a non-local address needs confirmation.",
  "guidance.npx.title": "Install locally and connect (npx)",
  // 连起来读：Run `npx @deepseek-ai/dsh web` in a terminal to start a local harness service.
  "guidance.npx.step1.before": "Run",
  "guidance.npx.step1.after": "in a terminal to start a local harness service",
  // 连起来读：Once ready, the address and port appear in the terminal (e.g. `http://127.0.0.1:3456`)
  "guidance.npx.step2.before": "Once ready, the address and port appear in the terminal (e.g.",
  "guidance.npx.step2.after": ")",
  "guidance.npx.step3": "Open “Server” → switch to “Local/remote service” → enter that address → “Connect”",
  // 连起来读：Loopback addresses (127.0.0.1/localhost) need no security confirmation;
  // restart with `--host <LAN-IP>` for LAN access.
  "guidance.npx.hint.before": "Loopback addresses (127.0.0.1/localhost) need no security confirmation; restart with",
  "guidance.npx.hint.after": "for LAN access.",

  /* ---------- 加载页 ---------- */
  "loading.title": "Starting...",
  "loading.hint": "DeepSeek Harness is loading plugins and services, please wait",
  "loading.phase.starting": "Starting the service process, please wait",
  "loading.phase.serving": "Plugins are ready, starting the service port",
  "loading.progress": "Loaded {loaded}/{total} plugins",

  /* ---------- 状态栏 ---------- */
  // 状态栏各片段由代码首尾相接、不加分隔符，所以修饰语自带前导空格。
  "status.external": "External service",
  "status.externalWithHost": "External service {host}",
  "status.clientFailed": "UI plugins failed to load",
  "status.running": "Running",
  "status.runningWithHost": "Running {host}",
  "status.starting": "Starting",
  "status.failed": "Startup failed",
  "status.stopped": "Stopped",
  "status.suffix.safeMode": " (safe mode)",
  "status.suffix.freshHome": " (fresh environment)",
  "status.exitCode": " ({code})",

  /* ---------- 预检页 ---------- */
  "preflight.title": "Preflight checks…",
  "preflight.checking": "Checking the environment, configuration and plugins, one moment",
  "preflight.deepRepair": "Deep repair (removes problem plugins)",
  "preflight.safeMode": "Start in safe mode",
  "preflight.freshHome": "Start with a fresh environment",
  "preflight.skip": "Ignore issues and start anyway",
  "preflight.freshHomeConfirm": "Starts with a fresh runtime environment (~/.dsh-fallback):\n\n· Session history, model settings and third-party plugins are not visible\n· Credentials such as the API key are not migrated and must be configured again\n· The original ~/.dsh data is left untouched, so you can return to the default environment at any time\n\nContinue?",


  /* ---------- 启动失败页 ---------- */
  "failed.title": "Startup failed",
  "failed.diagnose": "Diagnose",
  "failed.safeMode": "Start in safe mode",
  // 冒号后由 HTML 接上日志路径。
  "failed.logHint": "Full log:",

  /* ---------- 服务器弹框 ---------- */
  "server.title": "Server",
  "server.mode.label": "Connection mode",
  "server.mode.container": "In container",
  "server.mode.external": "Local/remote service",
  "server.state.label": "Status",
  "server.address.label": "Address",
  "server.copyAddress": "Copy service address",
  "server.copied": "Copied",
  "server.start": "Start",
  "server.restart": "Restart",
  "server.stop": "Stop",
  "server.safeMode.start": "Start in plugin safe mode",
  "server.safeMode.hint": "Skips third-party plugins installed later, keeping your sessions and settings. Worth trying if startup fails after an upgrade.",
  "server.safeMode.active": "Running in plugin safe mode",
  "server.safeMode.exit": "Exit safe mode",
  "server.freshHome.active": "Running with a fresh environment (existing ~/.dsh data preserved)",
  "server.freshHome.exit": "Back to the default environment",
  "server.ext.address": "Service address",
  "server.ext.connect": "Connect",
  "server.ext.disconnect": "Disconnect",

  /* ---------- 工具链市场弹框 ---------- */
  "tools.title": "Toolchain market",
  "tools.search": "Search toolchains",
  "tools.refreshIndexTitle": "Refresh the remote index",
  "tools.refreshIndex": "Refresh index",
  "tools.updateBanner.count": "{count} toolchains can be updated",
  "tools.updateBanner.withTargets": "{count} toolchains can be updated: {targets}",
  "tools.updateBanner.title": "One-click update switches to these versions: {targets}; older versions stay on disk and can be switched to or uninstalled from a card's dropdown",
  "tools.updateAll": "Update all",
  "tools.gridLabel": "Toolchain list",
  "tools.builtin": "Bundled",
  "tools.builtinLabel": "Bundled toolchain list",
  "tools.builtinEmpty": "No bundled toolchain information",
  "tools.builtinInstalled": "Bundled with the package",
  "tools.builtinMissing": "Bundled toolchain missing",
  "tools.hostsLabel": "Host import",
  "tools.hostsTitle": "Host import (Linglong sandbox; takes effect after a restart)",
  "tools.hostScan": "Scan host",
  "tools.hostPathPlaceholder": "Host toolchain directory, e.g. /usr/lib/jvm/java-21",
  "tools.hostNamePlaceholder": "Name (optional)",
  "tools.hostAdd": "Mount",
  "tools.recheck": "Check again",
  "tools.devNotice": "Development mode: host commands are already on PATH; host import only works in a packaged Linglong environment.",
  "tools.category.all": "All",
  "tools.category.languageSdk": "Language SDKs",
  "tools.category.buildTools": "Build & compile",
  "tools.category.modernCli": "Modern CLI",
  "tools.category.codeQuality": "Code quality",
  "tools.category.debug": "Debugging",
  "tools.empty": "No matching toolchains",
  "tools.emptyHint": "Try another keyword, or clear the current category filter.",
  "tools.clearFilters": "Clear filters",
  "tools.pill.update": "Update",
  "tools.pill.installed": "✓ Installed",
  "tools.pill.installing": "Installing",
  "tools.pill.installable": "Installable",
  "tools.card.updateHint": "Can update to v{version}: use “Update all” at the top; the older version stays on disk and can be switched to or uninstalled from the dropdown",
  "tools.runtime.available": "Already available in the container: {cmd} ({source})",
  "tools.runtime.availableVersion": "Already available in the container: {cmd} {version} ({source})",
  "tools.runtime.title": "This command comes from the Linglong container environment and is not installed from the market repository; after installing it from the market, ~/.dsh-tools manages it and takes priority when PATH is injected",
  "tools.versionSelect.title": "Picking an installed version switches to it; click “Install” for a version that is not installed",
  "tools.version.current": " · current",
  "tools.version.installed": " · installed",
  "tools.version.installable": " · installable",
  "tools.install": "Install",
  "tools.installWithSize": "Install ({size})",
  "tools.installVersion": "Install {version}",
  "tools.installing": "Installing…",
  "tools.uninstall": "Uninstall",
  "tools.uninstallConfirm": "Confirm uninstall?",
  "tools.statusSeparator": " · ",
  "tools.status.bundled": "Bundled {ok}/{total}",
  "tools.status.installed": "{installed}/{total} toolchains installed",
  "tools.status.totalSize": "Total {size}",
  "tools.status.hostMounts": "{count} host mounts",
  "tools.status.filtered": "{count} matching",
  "tools.hostsSummary": "{count} mounted",
  "tools.hostRemove": "Remove",
  "tools.hostMounted": "✓ Active",
  "tools.hostPending": "Config written · takes effect after a restart",
  "tools.hostScanEmpty": "No importable host toolchain found (put tool directories under /opt, /usr/local or ~/tools, then scan again)",
  "tools.hostConflict": "Name clashes with an installed one",
  "tools.hostMountWarning": "After mounting, every process in the sandbox can read {path} (read-only); click “Confirm mount” once more to apply",
  "tools.hostMountConfirm": "Confirm mount?",
  "tools.hostMountFailed": "Mount failed: {error}",
  "tools.hostMountWritten": "Mount config written; restart the app to apply",
  "tools.hostMountWrittenWithWarning": "⚠ {warning}　Mount config written; restart the app to apply",
  "tools.refreshing": "Refreshing…",
  "tools.updating": "Updating…",
  "tools.updateFailed": "Update failed: {error}",
  "tools.scanning": "Scanning…",


  /* ---------- 关于弹框 ---------- */
  "about.title": "About",
  "about.harnessVersion": "DSH version",
  "about.upstreamRepo": "Upstream DSH repository",
  "about.packageVersion": "Linglong package version",
  "about.packager": "Linglong packager",
  "about.repo": "Linglong packaging repo",

  /* ---------- 浏览器预览分支 ---------- */
  "preview.noWails": "Wails runtime not detected (browser preview mode)",
};
