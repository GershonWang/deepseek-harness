/**
 * 文件职责：壳脚本的引导入口——先装 v2 全局适配层，再加载壳脚本本身。
 *
 * 为什么需要这一层：壳脚本是普通脚本（不 import 任何模块），而 v3 的 runtime 只能以
 * ES module 方式导入。用动态 import 串起「适配层 → 壳脚本」，就能保证顺序，且壳文件
 * 一个字都不用改。真实客户端迁移时，index.html 里只需要把原来的
 *   <script src="./app.js"></script>
 * 换成
 *   <script type="module" src="./boot.js"></script>
 */
import "./v2shim.js";

// 动态 import 保证在 shim 求值完成之后再生效；壳脚本因此可以像 v2 那样直接使用
// window.go / window.runtime。
await import("./app.js");
