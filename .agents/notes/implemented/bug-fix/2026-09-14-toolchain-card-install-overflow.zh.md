# Agent Note: 工具链市场的「安装中」卡片溢出到下一排

Status: implemented

[English](2026-09-14-toolchain-card-install-overflow.md) | 中文

## 问题

市场里任一卡片处于安装中时，动作行（版本下拉与安装按钮）会画到卡片边框之外、压在同排下方的卡片上。用户截图的测量为证：JDK 卡片盒纵跨 y 170–304（约 141px），动作行在 y 291–317，下一排卡片顶边在 y 321——整个动作行都在边框之外，版本下拉又从那个越界位置展开。

安装中状态比常规卡片多渲染两行（`frontend/app.js` 的 `el.append(head, desc, meta, progress, pctLabel, actions)`）。WebKitGTK 对 auto 网格行的自动尺寸取的是条目的「最小贡献」，对这些卡片就是 `min-height` 下限，于是超出下限的内容只会溢出、不会撑高行。既有的对策是按卡片形状设 `min-height` 档位（`.has-runtime` → 160px），安装中这一形状没有档位。

同一张截图还暴露出第二个缺陷：浅色主题下进度条轨道画的是 `--bg-input`（#ffffff），也就是卡片自身的背景色，于是只有填充可见。

## 决策

`frontend/styles.css` 三处改动：

1. `.market-grid` 显式声明 `grid-auto-rows: max-content`。认这个值的引擎按内容撑高行，`min-height` 档位退化为下限。
2. 安装中形状获得自己的档位：`.tool-card-item.installing { min-height: 176px }` 与 `.tool-card-item.installing.has-runtime { min-height: 198px }`。取值以预览门禁在 Chromium 实测的内容高（172 / 194）为下限再加 4px；实机内容高约 160 / 182，两套引擎都装得下。
3. `.tool-progress` 改画 `--bg-hover`（浅色 #e0e0e0、深色 #37373d），两套配色下轨道都与卡片有区分。

## 为什么档位与 max-content 并存

WebKitGTK 是否认 `grid-auto-rows: max-content`，在这里无法验证：本地只有 Chromium，而它本来就会撑高行，恰好把差异掩盖掉。档位的有效性反而在真机上已被验证——实测行高正好等于 `min-height`，`.has-runtime` 的 160px 档位也是同一个理由存在的。因此一个是前向修复、一个是保底，二者不冲突：档位只是下限。

## 备选方案

**只改 `grid-auto-rows: max-content`。** WebKitGTK 若不认这个值，修复等于没做，等于把结果押在未验证的实现上，故不单独采用。

**只加档位。** 能修好当下这一形状，但下一个多出一行的卡片形状会重演同一个缺陷；`max-content` 消掉的是这一类失败。

**把进度条挪到元信息行的位置、百分比并进徽标。** 卡片会恒为四行，安装期间网格也不跳动，但要改 `app.js` 的呈现与文案，而那里的注释主张「同一个数字不必出现三处」。这是设计取舍，不属于本次修复。

**把原生 `<select>` 换成自绘菜单。** WebKitGTK 的原生弹层必然覆盖下方卡片，本次修复之后它仍会盖住下一排顶部。自绘菜单的改动面大得多，本次不做。

## 后果

含安装中卡片的那一行会高约 35px，同排另外两张卡片随之变高、底部留出空白。这是行高共享的必然结果，与高度如何得出无关；只有「安装期间不额外占行」才能避免。

浅色主题下进度条轨道从此可见，同一张截图里的第二个缺陷一并修掉。

原生下拉仍会盖住下一排卡片的顶部。要彻底避免需要自绘菜单，本次未做。

## 测试

`node apps/desktop-launcher/frontend/tools/preview.mjs verify` 新增两条断言：每个卡片形状的 `min-height` 档位必须覆盖该形状的自然内容高；安装中形状的进度条轨道不得与卡片背景同色。变异检查：删掉两个 installing 档位会让四条断言失败（`档位 141/160 装不下内容 172/194`）；把轨道色改回 `--bg-input` 会让浅色主题以「轨道不可见」失败；两者还原后门禁重新通过。

未验证：本地只有 Chromium，WebKitGTK 的钉行高行为与是否认 `max-content` 都没有在真机复跑。档位以实机内容高（约 160 / 182）为下限、以 Chromium 实测加 4px 定档；真机确认需要重新构建 launcher 后目视。

## 关联

- [工具链市场的呈现](../feature/2026-09-12-desktop-launcher-toolchain-market-presentation.zh.md) 拥有弹框的卡片布局与控件样式。
- [工具链市场的 JDK 多版本清单](../feature/2026-09-14-toolchain-market-jdk-multiversion.zh.md) 是这个缺陷暴露出来的场景——新的版本下拉让溢出的那一行更显眼——但缺陷不是它引入的。
