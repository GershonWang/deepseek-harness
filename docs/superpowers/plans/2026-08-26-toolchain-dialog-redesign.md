# 工具链弹框重设计 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.


**Goal:** 把桌面启动器「工具链」弹框改造成 DeepSeek 品牌风（#4D6BFE、双主题）的「摘要条 + 三张分区卡」布局，随包工具显示子命令详情副文本，并新增 uv 一键安装项（uab 体积 0 增加）。


**Architecture:** 纯静态前端改造：只改 apps/desktop-launcher/frontend/ 下 index.html/styles.css/app.js 三个文件（壳 UI 保持无构建链 + go:embed）。展示层零 Go 改动；唯一后端增补是 internal/toolchain/catalog.go 的 Catalog() 新增 uv 安装项，以及 linglong/tools.yaml installable 段同步（与现有 jdk21/go/ripgrep 同一按需安装机制）。


**Tech Stack:** Go（Wails 启动器）、原生 HTML/CSS/JS（无框架）、CSS 变量 + prefers-color-scheme、color-mix。


---


## 文件结构


| 文件 | 职责 |
|---|---|
| apps/desktop-launcher/frontend/index.html | 工具链弹框 DOM：摘要条、三张分区卡、底部操作（#tools-modal 块重写） |
| apps/desktop-launcher/frontend/styles.css | 品牌 token（--brand）、卡片/徽章/摘要条/状态点/焦点环组件样式，深/浅双主题 |
| apps/desktop-launcher/frontend/app.js | renderTools() 重写：摘要计数、状态徽章、随包详情副文本映射、安装按钮、挂载列表 |
| apps/desktop-launcher/internal/toolchain/catalog.go | Catalog() 新增 uv 安装项 |
| apps/desktop-launcher/internal/toolchain/catalog_test.go | 新增 uv 的查找/字段断言测试 |
| apps/desktop-launcher/linglong/tools.yaml | installable 段新增 uv（与 catalog 同步，verify-tools.sh 校验 sha256 非占位） |

---


### Task 1: uv 一键安装项（Catalog + tools.yaml）


**Files:**
- Modify: apps/desktop-launcher/internal/toolchain/catalog_test.go
- Modify: apps/desktop-launcher/internal/toolchain/catalog.go
- Modify: apps/desktop-launcher/linglong/tools.yaml

- [ ] **Step 1: 写失败测试** —— 在 catalog_test.go 追加：
    func TestCatalog_Uv(t *testing.T) {
        it, ok := Lookup("uv")
        if !ok {
            t.Fatal("catalog 应含 uv")
        }
        if it.SHA256 == "" {
            t.Fatal("uv 应已填实 sha256")
        }
        if it.BinRel != "." {
            t.Fatalf("uv tarball 单顶层目录剥离后可执行在根，BinRel 应为 .: %+v", it)
        }
        if it.Version != "0.12.6" {
            t.Fatalf("uv 版本应为 0.12.6: %+v", it)
        }
    }

- [ ] **Step 2: 运行确认失败**
    cd apps/desktop-launcher && go test ./internal/toolchain/ -run TestCatalog_Uv -v

Expected: FAIL —— Lookup("uv") 未命中（catalog 尚无 uv）。

- [ ] **Step 3: 实现 Catalog() 新增 uv** —— 在 catalog.go 的 Catalog() 中 ripgrep 条目之后追加：
            {
                Name:    "uv",
                Label:   "uv",
                Version: "0.12.6",
                URL:     "https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz",
                SHA256:  "8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477",
                BinRel:  ".",
            },


- [ ] **Step 4: 运行确认通过**
    cd apps/desktop-launcher && go test ./internal/toolchain/ -run 'TestCatalog_(Uv|Lookup|NoDuplicatedBundledTools|Statuses)' -v

Expected: 全 PASS（uv 不命中 TestCatalog_NoDuplicatedBundledTools 的内置集合；TestCatalogStatuses 无需改动）。随后跑全量 go test ./... 确认零回归。

- [ ] **Step 5: tools.yaml installable 段同步** —— 在 ripgrep 条目后追加：
      uv:
        version: "0.12.6"
        url: "https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz"
        sha256: "8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477"


- [ ] **Step 6: 运行 verify 测试**
    sh apps/desktop-launcher/linglong/test-verify-tools.sh

Expected: 4 项 PASS（verify-tools.sh 会校验 installable/sha256 非占位，uv 的 sha256 为真实值）。

- [ ] **Step 7: 提交**
    git add apps/desktop-launcher/internal/toolchain/catalog.go apps/desktop-launcher/internal/toolchain/catalog_test.go apps/desktop-launcher/linglong/tools.yaml
    git commit -m "feat(desktop-launcher): add uv to on-demand toolchain catalog"


---


### Task 2: 弹框 HTML 结构（摘要条 + 三张分区卡）


**Files:**
- Modify: apps/desktop-launcher/frontend/index.html（#tools-modal 块，现 129-171 行）

- [ ] **Step 1: 重写 #tools-modal** —— 用下面整块替换原 #tools-modal（删除原两张表格与 #tools-install，#hosttools-box 结构并入「宿主挂载」卡）：
    <!-- 工具链弹框 -->
    <div id="tools-modal" class="modal hidden">
      <div class="modal-card modal-tools">
        <div class="modal-head">
          <span class="modal-title"><span class="brand-dot"></span>工具链</span>
          <button class="modal-close" data-close="tools-modal" aria-label="关闭">×</button>
        </div>
        <div class="modal-body">
          <div id="tool-summary" class="tool-summary" role="status"></div>

          <section class="tool-card tool-card-live" aria-label="随包工具">
            <h3 class="section-title">随包工具</h3>
            <div id="bundled-list" class="tool-list"></div>
          </section>

          <section class="tool-card" aria-label="一键安装">
            <h3 class="section-title">一键安装</h3>
            <div id="catalog-list" class="tool-list"></div>
            <div id="toolchain-notice" class="hint"></div>
          </section>

          <section id="card-hosts" class="tool-card" aria-label="宿主挂载">
            <h3 class="section-title">宿主挂载</h3>
            <div class="host-row">
              <input id="host-path" type="text" placeholder="宿主路径，如 /usr/lib/jvm/java-21-openjdk-amd64" spellcheck="false">
              <input id="host-name" type="text" placeholder="名称(可选)" spellcheck="false">
              <button id="host-add" class="btn btn-primary">挂载</button>
            </div>
            <div id="host-list" class="host-list"></div>
            <div id="host-hint" class="hint"></div>
          </section>

          <div class="actions">
            <button id="tools-refresh" class="btn btn-quiet">重新检查</button>
          </div>
        </div>
      </div>
    </div>


约束：保留 tools-refresh、host-add、host-path、host-name、host-list、host-hint、toolchain-notice 的 id（app.js 事件绑定与渲染逻辑依赖）；新增 tool-summary、bundled-list、catalog-list、card-hosts。

- [ ] **Step 2: 静态预览确认结构** —— 浏览器打开 apps/desktop-launcher/frontend/index.html（无 Wails 时前端降级只显示引导页，但可打开开发者工具查看 DOM 与 CSS 不报错）。Expected: 无 JS 报错，新 id 存在。

- [ ] **Step 3: 提交**
    git add apps/desktop-launcher/frontend/index.html
    git commit -m "feat(desktop-launcher): restructure toolchain dialog into summary and three cards"


---


### Task 3: 品牌 token 与组件样式（深/浅双主题）


**Files:**
- Modify: apps/desktop-launcher/frontend/styles.css

- [ ] **Step 1: 品牌 token** —— :root 里把 --accent: #007acc; 替换为：
      --brand: #4d6bfe;
      --brand-strong: #6e8bff;
      --accent: var(--brand);
      --radius-lg: 10px;

并在 @media (prefers-color-scheme: light) 的 :root 块里追加：
        --brand: #2547d0;
        --brand-strong: #1b38b8;


- [ ] **Step 2: 追加组件样式** —— 在 styles.css 末尾追加（复用既有 .host-row/.host-item/.btn*/hint/.section-title）：
    /* 工具链弹框（品牌化改版） */
    .modal-card.modal-tools { width: 560px; }
    .modal-title { display: flex; align-items: center; gap: 8px; font-weight: 700; }
    .brand-dot { width: 9px; height: 9px; border-radius: 3px; background: var(--brand);
      box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand) 18%, transparent); }

    .tool-summary { display: flex; flex-wrap: wrap; gap: 7px; }
    .chip { font-size: 11px; font-weight: 600; border-radius: 99px; padding: 3px 11px;
      background: var(--bg-panel); border: 1px solid var(--border-strong); color: var(--fg-dim); }
    .chip-ok { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 40%, transparent);
      background: color-mix(in srgb, var(--ok) 8%, transparent); }
    .chip-warn { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 40%, transparent);
      background: color-mix(in srgb, var(--warn) 8%, transparent); }
    .chip-brand { color: var(--brand); border-color: color-mix(in srgb, var(--brand) 45%, transparent);
      background: color-mix(in srgb, var(--brand) 10%, transparent); }

    .tool-card { border: 1px solid var(--border-strong); border-radius: var(--radius-lg);
      background: var(--bg-panel); padding: 10px 12px; display: flex; flex-direction: column; gap: 6px; }
    .tool-card-live { border-color: color-mix(in srgb, var(--brand) 35%, var(--border-strong)); }
    .tool-card .section-title { margin: 0; }

    .host-list { display: flex; flex-direction: column; }
    .tool-list { display: flex; flex-direction: column; }
    .tool-row { display: flex; align-items: center; gap: 8px; padding: 4px 0; }
    .tool-row + .tool-row { border-top: 1px solid var(--border); }
    .tool-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
    .tool-version { color: var(--fg-dim); font-variant-numeric: tabular-nums; }
    .tool-detail { padding: 0 0 5px 17px; font-size: 10.5px; color: var(--fg-dim); }
    .tool-list .empty { padding: 8px 0; color: var(--fg-dim); font-size: 12px; }

    .state-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; display: inline-block; }
    .state-dot.ok { background: var(--ok); }
    .state-dot.missing { background: var(--danger); }
    .state-dot.brand { background: var(--brand); }

    .pill { font-size: 10px; font-weight: 600; border-radius: 99px; padding: 1.5px 8px; flex-shrink: 0; }
    .pill.ok { color: var(--ok); background: color-mix(in srgb, var(--ok) 10%, transparent);
      border: 1px solid color-mix(in srgb, var(--ok) 40%, transparent); }
    .pill.brand { color: var(--brand); background: color-mix(in srgb, var(--brand) 10%, transparent);
      border: 1px solid color-mix(in srgb, var(--brand) 45%, transparent); }
    .pill.warn { color: var(--warn); background: color-mix(in srgb, var(--warn) 10%, transparent);
      border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent); }
    .pill.danger { color: var(--danger); background: color-mix(in srgb, var(--danger) 10%, transparent);
      border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent); }

    .btn:focus-visible, .host-row input:focus-visible { outline: 2px solid var(--brand); outline-offset: 1px; }


说明：旧的 .tools 表格 CSS（.tools-wrap/.tools th/td）在新 DOM 中不再被引用，本轮保留不删（避免扩散改动）；unused 清理留给后续简化任务。

- [ ] **Step 3: 静态预览确认** —— 浏览器打开 index.html，开发者工具确认：无 CSS 语法错误；.modal-tools 宽度 560px；深色下 --brand 为 #4d6bfe，浅色（模拟 prefers-color-scheme: light）为 #2547d0。

- [ ] **Step 4: 提交**
    git add apps/desktop-launcher/frontend/styles.css
    git commit -m "feat(desktop-launcher): add brand tokens and toolchain dialog component styles"


---


### Task 4: renderTools 重写（摘要/徽章/详情副文本）


**Files:**
- Modify: apps/desktop-launcher/frontend/app.js（renderTools 函数，现 123-208 行）

- [ ] **Step 1: 在 renderTools 上方新增详情映射与两个小工具函数**：
    const TOOL_DETAILS = {
      node: "npm · npx · corepack · pnpm",
      python3: "pip · pip3",
      git: "git-lfs",
    };

    function pill(cls, text) {
      return "<span class='pill " + cls + "'>" + esc(text) + "</span>";
    }

    function dot(cls) {
      return "<span class='state-dot " + cls + "'></span>";
    }


- [ ] **Step 2: 整体替换 renderTools 函数体**：
    function renderTools(t) {
      // ------ 摘要条 ------
      const sum = $("#tool-summary");
      sum.innerHTML = "";
      const rows = t.Rows || [];
      const cats = t.Catalog || [];
      const chips = [];
      if (rows.length > 0) {
        const ok = rows.filter((r) => r.State === "installed").length;
        const all = ok === rows.length;
        chips.push("<span class='chip " + (all ? "chip-ok" : "chip-warn") + "'>随包 " + ok + "/" + rows.length + " ✓</span>");
      }
      if (cats.length > 0) {
        const installed = cats.filter((c) => c.State === "installed").length;
        const all = installed === cats.length;
        chips.push("<span class='chip " + (all ? "chip-ok" : "chip-brand") + "'>可安装 " + installed + "/" + cats.length + " ✓</span>");
      }
      if (t.Sandboxed && (t.HostTools || []).length > 0) {
        chips.push("<span class='chip'>挂载 " + t.HostTools.length + " 项</span>");
      }
      sum.innerHTML = chips.join("");

      // ------ 随包工具 ------
      const bl = $("#bundled-list");
      bl.innerHTML = "";
      if (rows.length === 0) bl.innerHTML = "<div class='empty'>无结果</div>";
      for (const row of rows) {
        const ok = row.State === "installed";
        const detail = TOOL_DETAILS[row.Name]
          ? "<div class='tool-detail'>" + esc(TOOL_DETAILS[row.Name]) + "</div>"
          : "";
        const el = document.createElement("div");
        el.className = "tool-row";
        el.innerHTML =
          (ok ? dot("ok") : dot("missing")) +
          "<span class='tool-name'>" + esc(row.Name) + "</span>" +
          "<span class='tool-version'>" + (ok ? esc(row.Version) : "—") + "</span>" +
          (ok ? pill("ok", "✓ 已安装") : pill("danger", "✗ 缺失"));
        bl.appendChild(el);
        if (detail) {
          const d = document.createElement("div");
          d.innerHTML = detail;
          bl.appendChild(d);
        }
      }

      // ------ 一键安装 ------
      const cl = $("#catalog-list");
      cl.innerHTML = "";
      if (cats.length === 0) cl.innerHTML = "<div class='empty'>无结果</div>";
      for (const c of cats) {
        const installed = c.State === "installed";
        const installing = !installed && t.Installing === c.Name;
        let statusPill = "";
        if (installed) statusPill = pill("ok", "✓ 已安装");
        else if (installing) statusPill = pill("warn", "安装中…");
        else if (c.Pinned) statusPill = pill("brand", "可安装");
        else statusPill = pill("warn", "待配置");
        const version = installed ? (c.InstalledVersion || c.Version) : c.Version;
        const el = document.createElement("div");
        el.className = "tool-row";
        el.innerHTML =
          (installed ? dot("ok") : dot("brand")) +
          "<span class='tool-name'>" + esc(c.Label) + "</span>" +
          "<span class='tool-version'>" + esc(version) + "</span>" +
          statusPill;
        if (!installed && c.Pinned) {
          const b = document.createElement("button");
          b.className = "btn";
          b.textContent = installing ? "安装中…" : "安装";
          b.disabled = installing;
          b.addEventListener("click", () => {
            b.disabled = true;
            b.textContent = "安装中…";
            api().InstallToolchain(c.Name);
          });
          el.appendChild(b);
        }
        cl.appendChild(el);
      }
      $("#toolchain-notice").textContent = t.Notice || "";

      // ------ 宿主挂载 ------
      const hostBox = $("#card-hosts");
      if (!t.Sandboxed) {
        hostBox.classList.add("hidden");
        $("#toolchain-notice").textContent = "开发态：宿主命令本就在 PATH，宿主挂载仅玲珑打包环境可用。";
        return;
      }
      hostBox.classList.remove("hidden");
      const hl = $("#host-list");
      hl.innerHTML = "";
      for (const h of t.HostTools || []) {
        const row = document.createElement("div");
        row.className = "host-item";
        const rm = document.createElement("button");
        rm.className = "btn btn-danger";
        rm.textContent = "移除";
        rm.addEventListener("click", () => api().RemoveHostTool(h.Name));
        const mounted = h.Mounted
          ? "<span class='state-ok'>✓ 生效中</span>"
          : "<span class='state-missing'>配置已写入 · 重启应用后生效</span>";
        row.innerHTML =
          "<span class='selectable host-name'>" + esc(h.Name) + "</span>" +
          "<span class='hint selectable'>" + esc(h.Source) + " → " + esc(h.Target) + "</span>" +
          "<span class='hint'>" + mounted + "</span>";
        row.appendChild(rm);
        hl.appendChild(row);
      }
      const hint = $("#host-hint");
      hint.textContent =
        "挂载为只读（工具箱需自写安装目录时不可用）；非家目录路径在部分系统环境可能挂载失败，" +
        "建议优先用上方一键安装或把工具放入家目录后再挂载；改动需重启应用生效。";
    }


约束：删除旧实现中引用 #tools-table/#catalog-table/#tools-install 的代码（这些元素已从 HTML 移除）。事件绑定（bindUI 中 tools-refresh/host-add）不变。

- [ ] **Step 3: 静态语法自检**
    node --check apps/desktop-launcher/frontend/app.js

Expected: 无输出（语法 OK）。

- [ ] **Step 4: 提交**
    git add apps/desktop-launcher/frontend/app.js
    git commit -m "feat(desktop-launcher): render toolchain summary, status pills and tool details"


---


### Task 5: 整体构建与人工验证


**Files:** 无（验证任务）

- [ ] **Step 1: Go 全量测试**
    cd apps/desktop-launcher && go test ./...

Expected: PASS（catalog 新增 uv 后无回归）。

- [ ] **Step 2: verify 测试**
    sh apps/desktop-launcher/linglong/test-verify-tools.sh

Expected: 4 项 PASS。

- [ ] **Step 3: 构建启动器并运行**
    cd apps/desktop-launcher
    make build
    ./dsh-desktop-launcher

Expected: 启动成功（开发态命中 appenv 三级回退）。

- [ ] **Step 4: 人工核对清单**（对照 spec「验证」）
1. 打开「工具链」弹框：摘要条「随包 N/N ✓」「可安装 N/N ✓」与三张分区卡出现；
2. 随包卡：git/python3/node/curl/jq/pnpm 带状态点与「✓ 已安装」徽章；node/python3/git 有子命令副文本（npm · npx · corepack · pnpm / pip · pip3 / git-lfs）；
3. 一键安装卡：jdk21/go/ripgrep/uv 行——已装者「✓ 已安装」无按钮，未装着「可安装」+「安装」按钮；点安装立即变「安装中…」且按钮禁用，成功后回「✓ 已安装」，失败显示「工具链 X 安装失败: …」（Notice 提示）；
4. 宿主挂载卡：开发态隐藏并显示「开发态：宿主命令本就在 PATH…」；沙箱态（玲珑包）显示挂载列表「✓ 生效中/重启后生效」与移除按钮；输入路径+名称点挂载后有提示；
5. 深/浅主题切换系统外观，弹框两套 token 均正常；按钮/输入框焦点出现品牌蓝描边；
6. 弹框宽度 560px，三卡纵向排布无溢出（窄窗口下 max-width 92vw 生效）。

- [ ] **Step 5: uv 一键安装实测（可选，需网络）** —— 沙箱/玲珑环境点 uv「安装」，等待下载 19.3MB 完成；Expected: ~/.dsh-tools/bin/uv 与 uvx 软链生成，重启应用后容器内 uv --version → 0.12.6。

- [ ] **Step 6: 提交验证期修复（如有）** —— 若验证发现缺陷，修复后按对应 Task 的提交规范补提交。

## 自检备注

- spec 覆盖：C 品牌（Task 3 token/样式）、B 信息组织（Task 2 结构 + Task 4 摘要）、D 交互（Task 4 徽章/禁用态 + Task 3 焦点环）、A 质感（Task 3 组件样式）；详情副文本（Task 4 TOOL_DETAILS）；uv（Task 1 + Task 5 Step 5）；双主题（Task 3 Step 1）。

- 无占位符：所有代码块为完整可实现内容，命令含预期输出。

- 类型一致：字段名与 app.go 的 ToolRow{Name,Version,State}/CatalogStatus{Name,Label,Version,InstalledVersion,State,Pinned}/HostToolEntry 一致；id 与 Task 2 HTML 一致。
