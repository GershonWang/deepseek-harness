// 本文件覆盖「关于」数据载荷：它是前端渲染的唯一来源，字段缺失或写错会让弹框
// 静默变成空白行（壳前端按空串展示，不会报错）。

package app

import "testing"

// TestAboutInfoFillsEveryField 确保载荷每个字段都有值。
//
// 版本号在开发态是 "dev"、harness 版本读不到时也是 "dev"——那是可接受的降级；
// 但包 ID、封装署名与两个仓库地址是常量，缺一个就说明改坏了。
func TestAboutInfoFillsEveryField(t *testing.T) {
	info := AboutInfo()

	cases := []struct {
		name  string
		value string
	}{
		{"Version", info.Version},
		{"HarnessVersion", info.HarnessVersion},
		{"PackageID", info.PackageID},
		{"Packager", info.Packager},
		{"UpstreamRepo", info.UpstreamRepo},
		{"ForkRepo", info.ForkRepo},
	}
	for _, c := range cases {
		if c.value == "" {
			t.Fatalf("%s 为空：关于弹框会显示一行空白", c.name)
		}
	}

	if info.PackageID != PackageID {
		t.Fatalf("PackageID = %q，期望 %q", info.PackageID, PackageID)
	}
	if info.Packager != Packager {
		t.Fatalf("Packager = %q，期望 %q", info.Packager, Packager)
	}
	if info.UpstreamRepo != UpstreamRepo {
		t.Fatalf("UpstreamRepo = %q，期望 %q", info.UpstreamRepo, UpstreamRepo)
	}
	if info.ForkRepo != GithubRepo {
		t.Fatalf("ForkRepo = %q，期望 %q", info.ForkRepo, GithubRepo)
	}
}
