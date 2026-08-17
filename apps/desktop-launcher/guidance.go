package main

import (
	"net/url"
	"sync"
)

// guidanceHTML 是 webview 在空闲态(容器内 harness 未运行、外部服务未连接)
// 时加载的引导页。纯静态 HTML,不依赖任何外部资源,随二进制分发。
// 文案与弹框/状态栏用同一套词汇(服务器、容器内、本机/远端服务、启动、连接)。
const guidanceHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DeepSeek Harness</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0; min-height: 100vh;
    display: flex; align-items: center; justify-content: center;
    font-family: "Noto Sans CJK SC", "Source Han Sans SC", system-ui, sans-serif;
    background-color: #f0f1f3; color: #1f2328;
  }
  @media (prefers-color-scheme: dark) {
    body { background-color: #101418; color: #e6e8eb; }
  }
  .card { max-width: 620px; padding: 24px; }
  h1 { font-size: 26px; margin: 0 0 4px; }
  .intro { font-size: 14px; line-height: 1.7; margin: 0 0 20px; opacity: .85; }
  .modes { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  @media (max-width: 640px) { .modes { grid-template-columns: 1fr; } }
  .modes section.wide { grid-column: 1 / -1; }
  section {
    border: 1px solid; border-radius: 10px; padding: 16px 18px;
    border-color: rgba(127, 135, 145, .35);
    background-color: rgba(127, 135, 145, .06);
  }
  h2 { font-size: 15px; margin: 0 0 10px; }
  ol { margin: 0; padding-left: 20px; }
  li { font-size: 13px; line-height: 1.8; }
  code { font-family: ui-monospace, "Noto Sans Mono CJK SC", monospace; font-size: 12px; }
  .hint { font-size: 12px; line-height: 1.7; opacity: .7; margin: 10px 0 0; }
  .foot { font-size: 12px; opacity: .7; margin: 18px 0 0; }
</style>
</head>
<body>
<div class="card">
  <h1>DeepSeek Harness</h1>
  <p class="intro">当前没有可用的 harness 会话:容器内服务未运行,外部服务未连接。选择一种方式开始使用(服务启动中时请稍候片刻)。</p>
  <div class="modes">
    <section>
      <h2>容器内</h2>
      <ol>
        <li>点击右下角「服务器」</li>
        <li>保持「容器内」模式</li>
        <li>点击「启动」并等待就绪</li>
      </ol>
    </section>
    <section>
      <h2>连接本机/远端服务</h2>
      <ol>
        <li>点击右下角「服务器」</li>
        <li>切换「本机/远端服务」</li>
        <li>填入服务地址,点击「连接」</li>
      </ol>
      <p class="hint">目标 harness 需以 <code>dsh web --host &lt;LAN-IP&gt;</code> 启动;非本机地址首次连接需确认。</p>
    </section>
    <section class="wide">
      <h2>本机安装并连接(npx)</h2>
      <ol>
        <li>在终端运行 <code>npx @deepseek-ai/dsh web</code> 启动本机 harness 服务</li>
        <li>就绪后服务地址与端口会显示在终端(如 <code>http://127.0.0.1:3456</code>)</li>
        <li>点「服务器」→ 切「本机/远端服务」→ 填入该地址 →「连接」</li>
      </ol>
      <p class="hint">本机回环地址(127.0.0.1/localhost)无需安全确认;需要局域网访问时加 <code>--host &lt;LAN-IP&gt;</code> 重新启动。</p>
    </section>
  </div>
  <p class="foot">底部状态栏会实时显示当前的连接状态与服务地址。</p>
</div>
</body>
</html>
`

// guidanceURL 返回引导页的 data: URL。静态内容以 PathEscape 编码,
// 不依赖文件系统与打包路径,任意工作目录下都可用;结果只计算一次。
func guidanceURL() string {
	guidanceOnce.Do(func() {
		guidanceCached = "data:text/html;charset=utf-8," + url.PathEscape(guidanceHTML)
	})
	return guidanceCached
}

var (
	guidanceOnce   sync.Once
	guidanceCached string
)

// resolveTarget 决定 webview 当前应加载的目标;guidance 为空时表示无引导页。
// 外部已连接优先于容器;容器仅在运行中接管,其余状态回引导页。
func resolveTarget(mode Mode, externalURL, containerURL string, running bool, guidance string) string {
	if mode == ModeExternal {
		return externalURL
	}
	if running {
		return containerURL
	}
	return guidance
}
