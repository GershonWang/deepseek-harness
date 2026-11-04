//go:build linux

package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 多处等长替换的前提：每个短路径都必须比被替换的原路径短、是绝对路径、不含空白。
// 脚本在容器里替换失败会直接中止构建，但那时已经跑了几分钟；这条用例让它在本地
// 就能被看到。
func TestShortPathsAreEqualLengthReplacements(t *testing.T) {
	cases := []struct {
		key  string
		orig string
	}{
		{"WEBKIT_EXEC", "/usr/lib/x86_64-linux-gnu/webkitgtk-6.0"},
		{"BWARP", "/usr/bin/bwrap"},
		{"DBUS_PROXY", "/usr/bin/xdg-dbus-proxy"},
	}

	paths := shortPaths()
	for _, c := range cases {
		short, ok := paths[c.key]
		if !ok || short == "" {
			t.Fatalf("%s 缺失：internal/packaging/short-paths.txt 未声明或为空", c.key)
		}
		if !strings.HasPrefix(short, "/") {
			t.Fatalf("%s 必须是绝对路径，得到 %q", c.key, short)
		}
		if strings.ContainsAny(short, " \t\r\n\x00") {
			t.Fatalf("%s 不能含空白或 NUL，得到 %q", c.key, short)
		}
		if len(short) > len(c.orig) {
			t.Fatalf("%s = %q 比原路径 %q 长，等长字节替换不成立", c.key, short, c.orig)
		}
	}
}

// TestPatchScriptReadsSharedShortPaths 守卫单源：打包脚本与 launcher 必须读同一个
// 短路径文件。脚本里再写一遍字面量的话，改一处就会产出「装得上、GUI 起不来」的包，
// 而打包与启动两个环节都不会报错。
func TestPatchScriptReadsSharedShortPaths(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "linglong", "patch-webkit-exec-path.sh")
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取打包脚本失败: %v", err)
	}
	body := string(script)
	if !strings.Contains(body, "../internal/packaging/short-paths.txt") {
		t.Fatalf("打包脚本未引用单一来源文件 short-paths.txt（改路径后必须同步）")
	}
	for _, key := range []string{"WEBKIT_EXEC", "BWARP", "DBUS_PROXY"} {
		if !strings.Contains(body, key) {
			t.Fatalf("打包脚本未处理 %s：短路径清单新增条目后必须同步脚本", key)
		}
	}
}
