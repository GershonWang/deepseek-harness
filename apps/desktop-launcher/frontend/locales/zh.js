/* 壳前端中文文案字典：**键全集的真源**。
 *
 * 为什么单独成文件：壳前端是零构建的全局脚本（见 docs/i18n.md 2.1），没有模块
 * 系统可依赖，字典只能挂在全局上按加载顺序组装；同时仓库闸门
 * `verify-client-ui-i18n` 的 `localeOwner()` 按路径含 `/locales/` 认定字典所有者，
 * 把文案集中在本目录才能让「禁止硬编码产品文案」的规则对壳生效。
 *
 * 约定（见 docs/i18n.md 6.2）：
 * - 键为点分命名空间，如 `titlebar.server`；
 * - 值里的 `{name}` 是占位符，由 i18n.js 的 format 替换，整句进字典而不是在
 *   代码里拼词序——英文语序与中文不同；
 * - 本文件是键全集，`en.js` 必须与它同键集，缺键由闸门直接判失败。
 *
 * P0 只建立机制，尚未迁入任何文案（见 docs/i18n.md 第七节）；P1 起按区域分批迁入。
 */
"use strict";

window.DSH_LOCALES = window.DSH_LOCALES || {};
window.DSH_LOCALES.zh = {};
