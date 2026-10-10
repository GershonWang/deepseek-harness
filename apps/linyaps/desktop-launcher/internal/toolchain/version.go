package toolchain

import (
	"sort"
	"strings"
)

// maxVersionDigits 是单个数字段参与比较的最大位数。
// 清单里的版本号不可能有此量级；不设上限时畸形标签（如 30 位数字）会溢出 int 得到负值，
// 把「谁更新」判断彻底弄反，而这正是本文件存在的意义。
const maxVersionDigits = 9

// compareVersions 按版本号数值比较两个标签：a 小于 b 返回 -1，相等返回 0，a 大于 b 返回 1。
//
// 清单里的版本标签风格并不统一：Go 是 `1.23.2`，JDK 8 用 Adoptium 的发行标签 `8u504`，
// JDK 21 是 `21.0.12.1`。因此比较以 `.`、`-`、`+`、`_` 分段，逐段取前导数字做数值比较
// （`8u504` 的首段即 8，`u504` 只是发行标签的一部分，不参与大小判定），缺失的段按 0
// 处理——`1.2` 与 `1.2.0` 因此相等。
//
// 任一侧不含任何数字时不猜顺序，退回字节序比较：调用方（`HasUpdate` 判定、卸载回退）
// 只需要一个稳定且可解释的顺序，而按字典序给出「谁更新」的错误答案是更坏的结果。
// 该分支同时覆盖空串与 `latest` 这类非版本标签。
func compareVersions(a, b string) int {
	as, aok := versionSegments(a)
	bs, bok := versionSegments(b)
	if !aok || !bok {
		return strings.Compare(a, b)
	}
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := range n {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionSegments 把版本标签切成数字段：以 `.`、`-`、`+`、`_` 分段，段内的每个数字串依次
// 各占一段——`v18` 取 18，`8u504` 取 (8, 504)，因此 JDK 8 的 build 号也参与比较。整串不含
// 数字时返回 ok=false，由调用方决定退化行为。
//
// 已知边界：预发布后缀（如 `1.0.0-rc1`）里的数字会被当作普通段参与比较，`rc1` 于是排在
// `1.0.0` 之后。清单当前没有此类标签；真出现时需要为后缀单独定序，而不是让 rc 的数字冒充补丁号。
func versionSegments(v string) ([]int, bool) {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '+' || r == '_'
	})
	segs := make([]int, 0, len(fields))
	any := false
	for _, f := range fields {
		nums := fieldNumbers(f)
		if len(nums) == 0 {
			segs = append(segs, 0)
			continue
		}
		any = true
		segs = append(segs, nums...)
	}
	return segs, any
}

// fieldNumbers 返回段内所有数字串的十进制值，按出现顺序。单串超过 maxVersionDigits 位时
// 只保留高位：再多只消耗不累加，避免畸形标签把 int 溢出成负值、把版本顺序彻底弄反。
func fieldNumbers(f string) []int {
	var out []int
	for i := 0; i < len(f); {
		if f[i] < '0' || f[i] > '9' {
			i++
			continue
		}
		n, digits := 0, 0
		for i < len(f) && f[i] >= '0' && f[i] <= '9' {
			if digits < maxVersionDigits {
				n = n*10 + int(f[i]-'0')
			}
			digits++
			i++
		}
		out = append(out, n)
	}
	return out
}

// sortVersionsDesc 按版本号从高到低就地排序。
// 展示（卡片里已装版本的下拉）与回退选择（卸载后激活哪个版本）共用同一顺序，
// 避免同一份版本集合在两处呈现相反次序。
func sortVersionsDesc(vers []string) {
	sort.SliceStable(vers, func(i, j int) bool {
		return compareVersions(vers[i], vers[j]) > 0
	})
}

// highestVersion 返回版本集合里数值最高的一个；空集合返回空串。
func highestVersion(vers []string) string {
	best := ""
	for i, v := range vers {
		if i == 0 || compareVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}
