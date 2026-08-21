//go:build linux

package packaging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebkitHelperLinkUsable(t *testing.T) {
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "WebKitNetworkProcess"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if webkitHelperLinkUsable(filepath.Join(t.TempDir(), "missing"), real) {
		t.Error("链接不存在时应不可用")
	}

	mkLink := func(target string) string {
		p := filepath.Join(t.TempDir(), "dsh-webkit-4.1")
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if link := mkLink(real); !webkitHelperLinkUsable(link, real) {
		t.Error("指向真实 helper 目录的链接应可用")
	}

	stale := filepath.Join(t.TempDir(), "uninstalled-app")
	if link := mkLink(stale); webkitHelperLinkUsable(link, real) {
		t.Error("悬空链接应不可用")
	}

	other := t.TempDir()
	if link := mkLink(other); webkitHelperLinkUsable(link, real) {
		t.Error("指向非当前包目录的链接应不可用")
	}
	if err := os.WriteFile(filepath.Join(other, "WebKitNetworkProcess"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if link := mkLink(other); !webkitHelperLinkUsable(link, other) {
		t.Error("指向具备 helper 的目录应可用")
	}
}
