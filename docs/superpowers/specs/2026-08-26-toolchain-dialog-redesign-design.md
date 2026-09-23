# Toolchain dialog redesign

English | [中文](2026-08-26-toolchain-dialog-redesign-design.zh.md)

Date: 2026-08-26 Status: Confirmed (layout direction B, the purely static option A, dual theme)

## Background

The desktop launcher's (apps/desktop-launcher) "Toolchain" dialog is currently two plain tables plus two lines of hint text crammed into a 520px dialog:
- "Toolchain self-check": a three-column tool/version/status table of the bundled tools (git/python3/node/curl/jq/pnpm);
- "Environment commands / toolchain": a four-column table of the on-demand install inventory (jdk21/go/ripgrep), plus a host-path mount input area.

After testing the Linglong package build, the user wants the dialog's appearance and information organization improved. Priority: **C (brand consistency) > B (information organization) > D (interaction detail) > A (visual polish)**.

## Goals

1. **Brand consistency**: align the current VS Code blue (#007acc) with the DeepSeek brand blue #4D6BFE (the brand color used by DSH badges across the repository), in both dark and light themes.
2. **Information organization**: switch to a vertical grouping-flow layout of "summary bar + three section cards":
   - Bundled tools (status list)
   - One-click install (installable entries + install button)
   - Host mounts (shown only in the sandbox environment)
3. **Interaction detail**: status badges (✓ installed / installable / installing… / ✗ missing / takes effect after restart), button disabled state, focus ring, hover feedback.
4. **Dual theme**: keep the existing prefers-color-scheme automatic switching and polish both dark and light.

## Non-goals (Out of scope)

- Zero Go changes in the presentation layer (the existing ToolStatus fields already cover all display data); the only Go change is a new uv one-click install entry in `Catalog()` (see below).
- No build chain (Vite/Tailwind) or component library (FAST/Shoelace) — the shell UI keeps its static HTML/CSS/JS + go:embed build-free architecture.
- No iconography (a per-tool icon is out of scope).
- No new frontend test infrastructure (the frontend has no test environment; it relies on Go tests + manual verification).

## File scope

Only three static files under apps/desktop-launcher/frontend/ change:

| File | Change |
|---|---|
| index.html | Rewrite the #tools-modal dialog structure (summary bar + three cards + bottom actions) |
| styles.css | Replace --accent with the brand token (--brand), add card/badge/summary-bar/focus-ring styles, dark/light dual theme |
| app.js | Rewrite renderTools(): generate the summary counts, status badges, install button, and mount list from the new DOM structure; a static mapping table for the bundled-tool detail subtext |
| internal/toolchain/catalog.go | Catalog() gains the uv one-click install entry (Label/Version/URL/SHA256/BinRel) |
| linglong/tools.yaml | The installable section gains uv (in sync with catalog.go; verify-tools.sh checks that sha256 is not a placeholder) |

## Dialog structure (HTML skeleton)

```text
工具链（头部：品牌蓝小方标 + 标题 + ✕）
├─ 摘要条（chips）：随包 N/N ✓ · 可安装 N/N ✓ · 挂载 N 项（按需隐藏）
├─ 卡1 随包工具：逐行 状态点 + 名称 + 版本 + 状态徽章
├─ 卡2 一键安装：逐行 名称 + 版本 + 状态徽章 + 安装按钮（按需）
├─ 卡3 宿主挂载（仅沙箱环境显示）：输入行 + 挂载列表 + 移除按钮
└─ 底部：重新检查（次要按钮，右对齐）
```

Each section card has its own small heading and a count description slot. The dialog width grows from 520px to ~560px to fit the three-card vertical layout (no more than 92vw).

## Status mapping (honest about the data; invent no state the backend does not have)

### Bundled tools (ToolStatus.Rows: ToolCheck{Name, OK, Version, Err})

| Condition | State dot | Badge | Version column |
|---|---|---|---|
| OK == true | green | ✓ installed (ok) | Version |
| OK == false | red | ✗ missing (danger) | — |

### One-click install (ToolStatus.Catalog: CatalogStatus{Name, Label, Version, InstalledVersion, State, Pinned}, together with ToolStatus.Installing)

| Condition | Badge | Button |
|---|---|---|
| State == installed | ✓ installed (ok) | none |
| not installed and Pinned | installable (brand) | "Install" (clickable) |
| not installed and !Pinned | awaiting configuration (warn, matching the existing "sha256 not configured") | none |
| Installing == Name | installing… (warn) | "installing…" disabled |

Version column display: use InstalledVersion when installed, otherwise the catalog Version.

### Host mounts (ToolStatus.HostTools: HostToolEntry{Name, Source, Target, Mounted})

| Condition | Badge | Action |
|---|---|---|
| Mounted == true | ✓ active (ok) | Remove (a danger secondary button) |
| Mounted == false | takes effect after restart (warn) | Remove |

- Non-sandbox environment (Sandboxed == false): the whole mount card is hidden and the existing hint is kept — "development mode: host commands are already on PATH, host mounts work only in the Linglong package environment."; if there is also an install-result Notice, it is appended after that hint (the install button is likewise usable in development mode, and the install result must not be overwritten by the static hint).
- Mount failure/success hints reuse the existing host-hint interaction (inserted at the bottom of the mount card after AddHostTool returns).

### Summary bar

- Bundled: `随包 {okCount}/{total} ✓`, the ok style only when everything is ready, otherwise warn.
- Installable: `可安装 {installed}/{total} ✓`, the ok style only when everything is ready, otherwise the brand/ins style.
- Mounts: `挂载 {n} 项`, shown only when Sandboxed and HostTools is non-empty.

### Hints and errors

- ToolStatus.Notice (one-off hints such as the install result) shows at the bottom of card 2 in the neutral hint style.
- The existing #tools-refresh (re-check) and #host-add (mount) event bindings stay unchanged; only the DOM structure is renewed.

## Bundled-tool details (in-row subtext, option A)

In the "Bundled tools" card, a tool with attached subcommands/capabilities renders a line of small subtext below its name row; a tool without them renders nothing. The data is a frontend static mapping table (bound to the Linglong package contents), while the detected state still follows ToolStatus.Rows:

| Tool | Detail subtext |
|---|---|
| node | npm · npx · corepack · pnpm |
| python3 | pip · pip3 |
| git | git-lfs |
| curl / jq / xxd / wget / zip / unzip / tar | renders no subtext |

> Note: uv is not bundled (see the next section) and will not appear in the bundled details; it is presented as an installable entry in "One-click install".

## uv on-demand install (approach 1, new)

Measured data (uv 0.12.6, 2026-08): the official gnu tarball is 19.3 MB, and after unpacking the uv binary is 48.7 MB plus uvx 0.3 MB. Bundling it into the uab would add an estimated 20–25 MB and lock the version to the package (uv releases monthly and often); so it joins the "One-click install" card instead, adding 0 to the uab size and using the same mechanism as jdk21/go/ripgrep.

New entry in Catalog():

| Field | Value |
|---|---|
| Name | uv |
| Label | uv |
| Version | 0.12.6 |
| URL | https://github.com/astral-sh/uv/releases/download/0.12.6/uv-x86_64-unknown-linux-gnu.tar.gz |
| SHA256 | 8681d8921e7d520fb368991dcf5f9c1905b80f5bf2a265a0ed085c8d8e342477 |
| BinRel | . (the tarball's single top-level directory is stripped by Install; uv/uvx sit at the unpack root, and LinkBin symlinks both into .dsh-tools/bin) |

The installable section of linglong/tools.yaml gains an entry of the same name in sync (version/url/sha256), keeping it consistent with the runtime catalog; verify-tools.sh's non-placeholder sha256 check covers it.

## Styles (CSS)

### Brand tokens

```css
:root {
  --brand: #4d6bfe;          /* DeepSeek 品牌蓝（深色主题） */
  --brand-strong: #6e8bff;   /* hover/按压 */
  --brand-text: var(--brand-strong); /* 品牌文字色：深色下用亮蓝满足 4.5:1 对比度 */
  --accent: var(--brand);    /* 既有使用点统一对齐品牌蓝 */
  --radius-lg: 10px;         /* 分区卡圆角 */
}
@media (prefers-color-scheme: light) {
  :root {
    --brand: #2547d0;        /* 浅色下加深保证对比度 */
    --brand-strong: #1b38b8;
    --brand-text: var(--brand);
    /* 删除浅色块遗留的 --accent: #0066b8;，两主题统一继承 --accent: var(--brand) */
  }
}
```

### Component styles

- **Section cards**: keep the existing .section border/background variable system and add .tool-card (--radius-lg, --bg-panel, padding 10px 12px).
- **Summary chips**: rounded pills with a light background plus a brand/green border; the ok state uses --ok and the warn state uses --warn.
- **Status badge pills**: a color-mix(in srgb, var(--ok/warn/danger) 12%, transparent) background plus a translucent border in the same hue (the existing CSS already uses color-mix, see .btn-danger:hover, so WebKit compatibility has precedent). Brand text (the color of .chip-brand/.pill.brand) uniformly uses --brand-text, which in the dark theme is --brand-strong (#6e8bff, a contrast ratio of ~4.96:1, passing) and in the light theme is --brand (#2547d0).
- **State dots**: 7px circles in the green/red/blue semantic colors.
- **Button hierarchy**: the primary action "Install" = a solid brand button; the secondary action "re-check" = a ghost button; the danger "Remove" = the existing btn-danger.
- **Focus ring**: input/button focus is outline: 2px solid var(--brand).
- **Dialog width**: .modal-card 520px → 560px (a class dedicated to the toolchain dialog, .modal-tools, so other dialogs are unaffected).

## Verification

1. cd apps/desktop-launcher && go test ./... — zero unit/integration test regressions after adding uv to Catalog().
2. After make build, run in development mode and check the dialog by hand:
   - dark/light themes (switching the system theme);
   - all states: bundled ✓/✗, installable, installing (button disabled), mounts ✓ active / takes effect after restart;
   - mounting input/removal and re-check;
   - the mount card is hidden in non-sandbox development mode.
3. The Linglong artifact tree needs no change (neither linglong.yaml nor buildext touches the dialog); tools.yaml only adds uv to the installable section (verify-tools.sh checks that its sha256 is not a placeholder).
4. The uv one-click install flow: click "Install" for uv in the "One-click install" card → download 19.3 MB → verify the sha256 → unpack to .dsh-tools/current/uv → symlink uv/uvx into .dsh-tools/bin; after a successful install and app restart, which uv / uv --version work inside the container.

## Confirmed decision record

- Priority: C (brand) > B (information organization) > D (interaction detail) > A (visual polish).
- Dual theme: A — keep the prefers-color-scheme automatic switching and tune both sets.
- Layout: direction B "vertical grouping flow" (chosen from the visual companion mockup).
- Implementation path: option A "purely static rework": three static frontend files + adding the uv entry to Catalog() + syncing tools.yaml installable.
- Bundled-tool details: option A "in-row subtext" (node→npm·npx·corepack·pnpm, python3→pip·pip3, git→git-lfs).
- uv: joins one-click install (approach 1, downloaded on demand, adding 0 to the uab size).
