package hosttools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"JDK 21":       "jdk-21",
		"/usr/lib/jvm": "usr-lib-jvm",
		"a_b.c":        "a-b-c",
		"..--":         "",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
	// 长度截断 ≤32
	if got := SanitizeName(strings.Repeat("a", 60)); len(got) > 32 {
		t.Errorf("SanitizeName 超长未截断: len=%d", len(got))
	}
}

func TestSuggestName(t *testing.T) {
	if got := SuggestName("/usr/lib/jvm/java-21-openjdk-amd64"); got != "java-21-openjdk-amd64" {
		t.Errorf("SuggestName = %q", got)
	}
}

func TestAddListRemove(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()

	e, warn, err := Add(home, "", src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if e.Name != filepath.Base(src) {
		t.Errorf("默认名称应派生自路径末段: %q vs %q", e.Name, filepath.Base(src))
	}
	if e.Target != MountBase+"/"+e.Name {
		t.Errorf("target 应为 %s: %q", MountBase, e.Target)
	}
	if warn == "" {
		t.Error("源不在家目录下应返回非 home 警告")
	}
	// 文件格式与 linglong config.d 一致：rbind+ro、非 home 警告不落盘
	data, _ := os.ReadFile(fileFor(ConfigDir(home), e.Name))
	if !strings.Contains(string(data), `"destination": "`+e.Target+`"`) ||
		!strings.Contains(string(data), `"options"`) ||
		!strings.Contains(string(data), `"rbind"`) ||
		!strings.Contains(string(data), `"ro"`) {
		t.Errorf("配置格式异常（缺 rbind/ro）:\n%s", data)
	}
	if strings.Contains(string(data), "绑定挂载可能失败") {
		t.Errorf("非 home 警告不应写入配置文件:\n%s", data)
	}

	list := List(home)
	if len(list) != 1 || list[0].Name != e.Name {
		t.Fatalf("List: %+v", list)
	}
	if list[0].Mounted {
		t.Error("未实际挂载时 Mounted 应为 false")
	}

	if err := Remove(home, e.Name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := List(home); len(got) != 0 {
		t.Fatalf("Remove 后应为空: %+v", got)
	}
}

func TestAdd_RejectsBadInput(t *testing.T) {
	home := t.TempDir()
	// 不存在路径
	if _, _, err := Add(home, "x", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("不存在路径应失败")
	}
	// 文件而非目录
	f := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Add(home, "x", f); err == nil {
		t.Fatal("非目录路径应失败")
	}
}

func TestAdd_HomeSourceNoWarning(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "tools")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	e, warn, err := Add(home, "", src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if warn != "" {
		t.Errorf("家目录内源不应警告: %q", warn)
	}
	if e.Source != src {
		t.Errorf("source 应为绝对路径 %q, got %q", src, e.Source)
	}
}

func TestMounted(t *testing.T) {
	dir := t.TempDir()
	if !mounted(dir) {
		t.Error("存在的目录 mounted 应为 true")
	}
	if mounted(filepath.Join(dir, "nope")) {
		t.Error("不存在的路径 mounted 应为 false")
	}
}

func TestEffectiveBin(t *testing.T) {
	root := t.TempDir()
	// <root>/bin 优先
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveBin(root); got != bin {
		t.Errorf("EffectiveBin 应优先 /bin: %q", got)
	}
	// 无 bin 但目录自身含可执行 -> 目录本身
	root2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(root2, "rg"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveBin(root2); got != root2 {
		t.Errorf("EffectiveBin 应回退到目录本身: %q", got)
	}
}
