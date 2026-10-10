package toolchain

import "testing"

// TestCompareVersions 固定版本比较的语义：这是「可更新」判定与卸载回退的共同基础，
// 一旦它按字典序退化，多版本下就会出现「更旧的版本被认为更新」的静默错误（AUDIT N20/N21）。
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		{"同版本相等", "21.0.12.1", "21.0.12.1", 0},
		{"JDK 8 低于 JDK 21", "8u504", "21.0.12.1", -1},
		{"JDK 17 低于 JDK 21", "17.0.20.1", "21.0.12.1", -1},
		{"JDK 17 高于 JDK 8", "17.0.20.1", "8u504", 1},
		{"缺失的段按 0 补齐", "1.2", "1.2.0", 0},
		{"补丁号参与比较", "1.2.1", "1.2.0", 1},
		{"两位数段按数值而非字典序", "1.10.0", "1.9.0", 1},
		{"前导 v 前缀不影响数值", "v18.19.0", "18.19.0", 0},
		{"携带 build 号的标签按 build 号比较", "8u504", "8u302", 1},
		{"整串无数字时退化为字节序", "latest", "1.0.0", 1},
		{"空串不 panic 且退化为字节序", "", "x", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s: compareVersions(%q, %q) = %d, want %d", c.name, c.a, c.b, got, c.want)
		}
	}
	// 反对称性：任意一对交换后符号相反，避免调用方依赖的参数顺序产生不同结论。
	for _, c := range cases {
		if got := compareVersions(c.b, c.a); got != -c.want {
			t.Errorf("%s: 交换参数后 compareVersions(%q, %q) = %d, want %d", c.name, c.b, c.a, got, -c.want)
		}
	}
}

func TestSortVersionsDesc(t *testing.T) {
	vers := []string{"8u504", "21.0.12.1", "17.0.20.1"}
	sortVersionsDesc(vers)
	want := []string{"21.0.12.1", "17.0.20.1", "8u504"}
	for i := range want {
		if vers[i] != want[i] {
			t.Fatalf("sortVersionsDesc = %v, want %v", vers, want)
		}
	}
}

func TestHighestVersion(t *testing.T) {
	if got := highestVersion([]string{"8u504", "17.0.20.1", "21.0.12.1"}); got != "21.0.12.1" {
		t.Fatalf("highestVersion = %q, want 21.0.12.1", got)
	}
	if got := highestVersion(nil); got != "" {
		t.Fatalf("空集合应返回空串, got %q", got)
	}
}
