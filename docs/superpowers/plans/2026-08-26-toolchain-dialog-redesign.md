# Toolchain dialog redesign — implementation plan

English | [中文](2026-08-26-toolchain-dialog-redesign.zh.md)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.


**Goal:** Rework the desktop launcher's "Toolchain" dialog into a DeepSeek-brand look (#4D6BFE, dual theme) with a "summary bar + three section cards" layout, show subcommand detail subtext for bundled tools, and add a one-click uv install entry (zero increase in uab size).


**Architecture:** A purely static frontend change: only the three files index.html/styles.css/app.js under apps/desktop-launcher/frontend/ change (the shell UI keeps its build-chain-free + go:embed setup). Zero Go changes in the presentation layer; the only backend addition is a new uv entry in Catalog() in internal/toolchain/catalog.go, plus keeping the installable section of linglong/tools.yaml in sync (the same on-demand install mechanism as the existing jdk21/go/ripgrep).


**Tech Stack:** Go (the Wails launcher), plain HTML/CSS/JS (no framework), CSS variables + prefers-color-scheme, color-mix.


---


## File structure


| File | Responsibility |
|---|---|
| apps/desktop-launcher/frontend/index.html | Toolchain dialog DOM: summary bar, three section cards, bottom actions (the #tools-modal block is rewritten) |
| apps/desktop-launcher/frontend/styles.css | Brand tokens (--brand), card/badge/summary-bar/state-dot/focus-ring component styles, dark/light dual theme |
| apps/desktop-launcher/frontend/app.js | renderTools() rewrite: summary counts, status badges, bundled-tool detail subtext mapping, install button, mount list |
| apps/desktop-launcher/internal/toolchain/catalog.go | Catalog() gains the uv install entry |
| apps/desktop-launcher/internal/toolchain/catalog_test.go | New lookup/field assertion tests for uv |
| apps/desktop-launcher/linglong/tools.yaml | installable section gains uv (in sync with the catalog; verify-tools.sh checks that sha256 is not a placeholder) |

---


### Task 1: uv one-click install entry (Catalog + tools.yaml)


**Files:**
- Modify: apps/desktop-launcher/internal/toolchain/catalog_test.go
- Modify: apps/desktop-launcher/internal/toolchain/catalog.go
- Modify: apps/desktop-launcher/linglong/tools.yaml

- [ ] **Step 1: Write the failing test** — append in catalog_test.go: func TestCatalog_Uv(t *testing.T) { it, ok := Lookup("uv") if !ok { t.Fatal("catalog 应含 uv") } if it.SHA256 == "" { t.Fatal("uv 应已填实 sha256") } if it.BinRel != "." { t.Fatalf("uv tarball 单顶层目录剥离后可执行在根，BinRel 应为 .: %+v", it) } if it.Version != "0.12.6" { t.Fatalf("uv 版本应为 0.12.6: %+v", it) } }

- [ ] **Step 2: Run it and confirm the failure** cd apps/desktop-launcher && go test ./internal/toolchain/ -run TestCatalog_Uv -v

Expected: FAIL — Lookup("uv") does not match (the catalog has no uv yet).

- [ ] **Step 3: Add uv to Catalog()** — after the ripgrep entry in Catalog() of catalog.go, append: { Name:    "uv", Label:   "uv", Version: "0.12.6", URL:     "https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz", SHA256:  "8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477", BinRel:  ".", },


- [ ] **Step 4: Run it and confirm it passes** cd apps/desktop-launcher && go test ./internal/toolchain/ -run 'TestCatalog_(Uv|Lookup|NoDuplicatedBundledTools|Statuses)' -v

Expected: all PASS (uv does not hit the built-in set of TestCatalog_NoDuplicatedBundledTools; TestCatalogStatuses needs no change). Then run the full go test ./... to confirm zero regressions.

- [ ] **Step 5: Sync the tools.yaml installable section** — after the ripgrep entry, append: uv: version: "0.12.6" url: "https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz" sha256: "8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477"


- [ ] **Step 6: Run the verify test** sh apps/desktop-launcher/linglong/test-verify-tools.sh

Expected: 4 items PASS (verify-tools.sh checks that installable/sha256 are not placeholders; uv's sha256 is a real value).

- [ ] **Step 7: Commit** git add apps/desktop-launcher/internal/toolchain/catalog.go apps/desktop-launcher/internal/toolchain/catalog_test.go apps/desktop-launcher/linglong/tools.yaml git commit -m "feat(desktop-launcher): add uv to on-demand toolchain catalog"


---


### Task 2: Dialog HTML structure (summary bar + three section cards)


**Files:**
- Modify: apps/desktop-launcher/frontend/index.html (the #tools-modal block, currently lines 129-171)

- [ ] **Step 1: Rewrite #tools-modal** — replace the original #tools-modal with the whole block below (this deletes the original two tables and #tools-install, and folds the #hosttools-box structure into the "Host mounts" card):
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


Constraint: keep the ids tools-refresh, host-add, host-path, host-name, host-list, host-hint, and toolchain-notice (app.js event binding and rendering depend on them); add tool-summary, bundled-list, catalog-list, and card-hosts.

- [ ] **Step 2: Confirm the structure with a static preview** — open apps/desktop-launcher/frontend/index.html in a browser (without Wails the frontend degrades to showing only the guidance page, but you can open the developer tools to inspect the DOM and CSS without errors). Expected: no JS errors and the new ids exist.

- [ ] **Step 3: Commit** git add apps/desktop-launcher/frontend/index.html git commit -m "feat(desktop-launcher): restructure toolchain dialog into summary and three cards"


---


### Task 3: Brand tokens and component styles (dark/light dual theme)


**Files:**
- Modify: apps/desktop-launcher/frontend/styles.css

- [ ] **Step 1: Brand tokens** — in :root, replace --accent: #007acc; with: --brand: #4d6bfe; --brand-strong: #6e8bff; --brand-text: var(--brand-strong);   /* brand text color: the bright blue keeps contrast in the dark theme */ --accent: var(--brand); --radius-lg: 10px;

and in the :root block of @media (prefers-color-scheme: light): --brand: #2547d0; --brand-strong: #1b38b8; --brand-text: var(--brand); Also delete the leftover --accent: #0066b8; in the light block (both themes now inherit --accent: var(--brand)).


- [ ] **Step 2: Append the component styles** — append at the end of styles.css (reusing the existing .host-row/.host-item/.btn*/hint/.section-title): /* toolchain dialog (branded revision) */ .modal-card.modal-tools { width: 560px; } .modal-title { display: flex; align-items: center; gap: 8px; font-weight: 700; } .brand-dot { width: 9px; height: 9px; border-radius: 3px; background: var(--brand); box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand) 18%, transparent); }

    .tool-summary { display: flex; flex-wrap: wrap; gap: 7px; } .chip { font-size: 11px; font-weight: 600; border-radius: 99px; padding: 3px 11px; background: var(--bg-panel); border: 1px solid var(--border-strong); color: var(--fg-dim); } .chip-ok { color: var(--ok); border-color: color-mix(in srgb, var(--ok) 40%, transparent); background: color-mix(in srgb, var(--ok) 8%, transparent); } .chip-warn { color: var(--warn); border-color: color-mix(in srgb, var(--warn) 40%, transparent); background: color-mix(in srgb, var(--warn) 8%, transparent); } .chip-brand { color: var(--brand-text); border-color: color-mix(in srgb, var(--brand) 45%, transparent); background: color-mix(in srgb, var(--brand) 10%, transparent); }

    .tool-card { border: 1px solid var(--border-strong); border-radius: var(--radius-lg); background: var(--bg-panel); padding: 10px 12px; display: flex; flex-direction: column; gap: 6px; } .tool-card-live { border-color: color-mix(in srgb, var(--brand) 35%, var(--border-strong)); } .tool-card .section-title { margin: 0; }

    .host-list { display: flex; flex-direction: column; } .tool-list { display: flex; flex-direction: column; } .tool-row { display: flex; align-items: center; gap: 8px; padding: 4px 0; } .tool-row + .tool-row { border-top: 1px solid var(--border); } .tool-detail + .tool-row { border-top: 1px solid var(--border); } .tool-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; } .tool-version { color: var(--fg-dim); font-variant-numeric: tabular-nums; } .tool-detail { padding: 0 0 5px 17px; font-size: 10.5px; color: var(--fg-dim); } .tool-list .empty { padding: 8px 0; color: var(--fg-dim); font-size: 12px; }

    .state-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; display: inline-block; } .state-dot.ok { background: var(--ok); } .state-dot.missing { background: var(--danger); } .state-dot.brand { background: var(--brand); }

    .pill { font-size: 10px; font-weight: 600; border-radius: 99px; padding: 1.5px 8px; flex-shrink: 0; } .pill.ok { color: var(--ok); background: color-mix(in srgb, var(--ok) 10%, transparent); border: 1px solid color-mix(in srgb, var(--ok) 40%, transparent); } .pill.brand { color: var(--brand-text); background: color-mix(in srgb, var(--brand) 10%, transparent); border: 1px solid color-mix(in srgb, var(--brand) 45%, transparent); } .pill.warn { color: var(--warn); background: color-mix(in srgb, var(--warn) 10%, transparent); border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent); } .pill.danger { color: var(--danger); background: color-mix(in srgb, var(--danger) 10%, transparent); border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent); }

    .btn:focus-visible, .host-row input:focus-visible { outline: 2px solid var(--brand); outline-offset: 1px; }


Note: the old .tools table CSS (.tools-wrap/.tools th/td) is no longer referenced by the new DOM; it is kept this round rather than deleted (to avoid spreading the change); unused-code cleanup is left to a later simplification task.

- [ ] **Step 3: Confirm with a static preview** — open index.html in a browser and confirm in the developer tools: no CSS syntax errors; .modal-tools is 560px wide; --brand is #4d6bfe in the dark theme and #2547d0 in the light theme (simulate prefers-color-scheme: light).

- [ ] **Step 4: Commit** git add apps/desktop-launcher/frontend/styles.css git commit -m "feat(desktop-launcher): add brand tokens and toolchain dialog component styles"


---


### Task 4: renderTools rewrite (summary/badges/detail subtext)


**Files:**
- Modify: apps/desktop-launcher/frontend/app.js (the renderTools function, currently lines 123-208)

- [ ] **Step 1: Add the detail map and two small helper functions above renderTools**: const TOOL_DETAILS = { node: "npm · npx · corepack · pnpm", python3: "pip · pip3", git: "git-lfs", };

    function pill(cls, text) { return "<span class='pill " + cls + "'>" + esc(text) + "</span>"; }

    function dot(cls) { return "<span class='state-dot " + cls + "'></span>"; }


- [ ] **Step 2: 整体替换 renderTools 函数体**： function renderTools(t) { // ------ 摘要条 ------ const sum = $("#tool-summary"); sum.innerHTML = ""; const rows = t.Rows || []; const cats = t.Catalog || []; const chips = []; if (rows.length > 0) { const ok = rows.filter((r) => r.State === "installed").length; const all = ok === rows.length; chips.push("<span class='chip " + (all ? "chip-ok" : "chip-warn") + "'>随包 " + ok + "/" + rows.length + " ✓</span>"); } if (cats.length > 0) { const installed = cats.filter((c) => c.State === "installed").length; const all = installed === cats.length; chips.push("<span class='chip " + (all ? "chip-ok" : "chip-brand") + "'>可安装 " + installed + "/" + cats.length + " ✓</span>"); } if (t.Sandboxed && (t.HostTools || []).length > 0) { chips.push("<span class='chip'>挂载 " + t.HostTools.length + " 项</span>"); } sum.innerHTML = chips.join("");

      // ------ 随包工具 ------
      const bl = $("#bundled-list");
      bl.innerHTML = "";
      if (rows.length === 0) bl.innerHTML = "<div class='empty'>无结果</div>";
      for (const row of rows) {
        const ok = row.State === "installed";
        const el = document.createElement("div");
        el.className = "tool-row";
        el.innerHTML =
          (ok ? dot("ok") : dot("missing")) +
          "<span class='tool-name'>" + esc(row.Name) + "</span>" +
          "<span class='tool-version'>" + (ok ? esc(row.Version) : "—") + "</span>" +
          (ok ? pill("ok", "✓ 已安装") : pill("danger", "✗ 缺失"));
        bl.appendChild(el);
        if (TOOL_DETAILS[row.Name]) {
          const d = document.createElement("div");
          d.className = "tool-detail";
          d.textContent = TOOL_DETAILS[row.Name];
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
          b.className = "btn btn-primary";
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
        const devMsg = "开发态：宿主命令本就在 PATH，宿主挂载仅玲珑打包环境可用。";
        $("#toolchain-notice").textContent = t.Notice ? devMsg + " " + t.Notice : devMsg;
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


Constraint: delete the code in the old implementation that references #tools-table/#catalog-table/#tools-install (those elements have been removed from the HTML). The event bindings (tools-refresh/host-add in bindUI) are unchanged.

- [ ] **Step 3: Static syntax self-check** node --check apps/desktop-launcher/frontend/app.js

Expected: no output (syntax OK).

- [ ] **Step 4: Commit** git add apps/desktop-launcher/frontend/app.js git commit -m "feat(desktop-launcher): render toolchain summary, status pills and tool details"


---


### Task 5: Full build and manual verification


**Files:** none (verification task)

- [ ] **Step 1: Full Go test run** cd apps/desktop-launcher && go test ./...

Expected: PASS (no regression after adding uv to the catalog).

- [ ] **Step 2: verify test** sh apps/desktop-launcher/linglong/test-verify-tools.sh

Expected: 4 items PASS.

- [ ] **Step 3: Build the launcher and run it** cd apps/desktop-launcher make build ./dsh-desktop-launcher

Expected: it starts (in development mode the three-level appenv fallback matches).

- [ ] **Step 4: Manual checklist** (against the spec's "Verification")
1. Open the "Toolchain" dialog: the summary bar "bundled N/N ✓" and "installable N/N ✓" and the three section cards appear;
2. Bundled card: git/python3/node/curl/jq/pnpm carry a state dot and the "✓ installed" badge; node/python3/git have subcommand subtext (npm · npx · corepack · pnpm / pip · pip3 / git-lfs);
3. One-click install card: the jdk21/go/ripgrep/uv rows — installed ones show "✓ installed" with no button, uninstalled ones show "installable" plus an "Install" button; clicking install immediately switches to "installing…" with the button disabled, then returns to "✓ installed" on success, and on failure shows "toolchain X install failed: …" (the Notice hint);
4. Host mount card: in development mode it is hidden and shows "development mode: host commands are already on PATH…"; in the sandbox (Linglong package) it shows the mount list "✓ active / takes effect after restart" and a remove button; entering a path plus name and clicking mount shows a hint;
5. Switch the system appearance between dark and light: both token sets in the dialog work; buttons and inputs get the brand-blue focus ring;
6. The dialog is 560px wide and the three cards stack vertically without overflow (max-width 92vw applies in narrow windows).

- [ ] **Step 5: Live test of the uv one-click install (optional, needs network)** — click "Install" for uv in the sandbox/Linglong environment and wait for the 19.3MB download to finish; Expected: ~/.dsh-tools/bin/uv and the uvx symlink are created, and uv --version inside the container reports 0.12.6 after the app restarts.

- [ ] **Step 6: Commit verification-period fixes (if any)** — if verification finds a defect, fix it and commit it following the commit convention of the matching Task.

## Self-check notes

- Spec coverage: C brand (Task 3 tokens/styles), B information organization (Task 2 structure + Task 4 summary), D interaction (Task 4 badges/disabled state + Task 3 focus ring), A polish (Task 3 component styles); detail subtext (Task 4 TOOL_DETAILS); uv (Task 1 + Task 5 Step 5); dual theme (Task 3 Step 1).

- No placeholders: every code block is complete and implementable, and the commands include their expected output.
- Follow-up (non-blocking): the ToolStatus.Installed/.Installable fields and catalogInstallable() called out by the final review are no longer used by the frontend (dead load) — they are kept rather than deleted to avoid widening the Go change; they can be cleaned up separately later, along with their unit tests.

- Type consistency: the field names match ToolRow{Name,Version,State}/CatalogStatus{Name,Label,Version,InstalledVersion,State,Pinned}/HostToolEntry in app.go; the ids match the Task 2 HTML.
