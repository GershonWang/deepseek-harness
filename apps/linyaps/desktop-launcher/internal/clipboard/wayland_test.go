package clipboard

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeWlPasteScript 是测试用的假 wl-paste：按 --type 参数回放预设内容。它让 Wayland
// 通道能在没有合成器、没有真实剪贴板的机器上被验证——真机上的 wl-paste 既要会话又要
// selection owner，无法在单元测试里提供。
//
// 文本类内容来自环境变量，图片走文件：shell 变量放不下 NUL 字节，而 PNG 压缩数据里
// 有 NUL。未声明的类型统一返回非零，与 wl-paste 在类型不存在时的行为一致。
const fakeWlPasteScript = `#!/bin/sh
type=""
while [ $# -gt 0 ]; do
  case "$1" in
    --type) type="$2"; shift 2 ;;
    *) shift ;;
  esac
done
case "$type" in
  text/uri-list) printf '%s' "$FAKE_URI_LIST" ;;
  x-special/gnome-copied-files) printf '%s' "$FAKE_GNOME_LIST" ;;
  image/png) cat "$FAKE_PNG" ;;
  *) exit 1 ;;
esac
`

// setUpFakeWlPaste 把假 wl-paste 放进临时目录并置顶 PATH，同时声明当前是 Wayland
// 会话。所有类型默认都取不到，由各用例只覆盖自己关心的那一个，从而也让「回退顺序」
// 变成可断言的行为而不是实现细节。
//
// PATH 是前置而不是替换：假脚本要能用到系统里的 cat。
func setUpFakeWlPaste(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "wl-paste")
	if err := os.WriteFile(script, []byte(fakeWlPasteScript), 0o755); err != nil {
		t.Fatalf("写入假 wl-paste 失败: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("FAKE_URI_LIST", "")
	t.Setenv("FAKE_GNOME_LIST", "")
	t.Setenv("FAKE_PNG", "")
}

// TestWaylandWlPaste 钉住唯一一处「命令是否可用」的判断：不在 Wayland 会话就返回空，
// 让纯 X11 会话不必白起进程；在会话内且命令在 PATH 上时返回其路径。
func TestWaylandWlPaste(t *testing.T) {
	t.Run("不在 Wayland 会话时返回空", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		if got := waylandWlPaste(); got != "" {
			t.Fatalf("期望空串，实际 %q", got)
		}
	})
	t.Run("命令在 PATH 上时返回其路径", func(t *testing.T) {
		dir := t.TempDir()
		script := filepath.Join(dir, "wl-paste")
		if err := os.WriteFile(script, []byte(fakeWlPasteScript), 0o755); err != nil {
			t.Fatalf("写入假 wl-paste 失败: %v", err)
		}
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		if got := waylandWlPaste(); got != script {
			t.Fatalf("期望 %q，实际 %q", script, got)
		}
	})
}

// TestReadWaylandImageBitmap 覆盖位图策略：命中真实 PNG 时返回它，而同一类型返回
// 非图片字节时必须被魔数校验拒掉——只凭「命令退出码为 0」不足以判断拿到了什么。
func TestReadWaylandImageBitmap(t *testing.T) {
	setUpFakeWlPaste(t)
	png := makeTestPNG(64, 48)

	pngPath := filepath.Join(t.TempDir(), "good.png")
	if err := os.WriteFile(pngPath, png, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	textPath := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(textPath, []byte("不是图片"), 0o644); err != nil {
		t.Fatalf("写入测试文本失败: %v", err)
	}

	t.Run("image/png 命中", func(t *testing.T) {
		t.Setenv("FAKE_PNG", pngPath)
		if got := readWaylandImage(); string(got) != string(png) {
			t.Fatalf("期望 %d 字节，实际 %d 字节", len(png), len(got))
		}
	})
	t.Run("image/png 返回非图片字节时拒收", func(t *testing.T) {
		t.Setenv("FAKE_PNG", textPath)
		if got := readWaylandImage(); got != nil {
			t.Fatalf("期望 nil，实际 %d 字节", len(got))
		}
	})
}

// TestReadWaylandUriListImage 覆盖文件策略：文件管理器复制图片文件时只给 URI 列表，
// 位图策略必然落空。用例同时钉住两种类型的回退顺序，以及「URI 指向非图片」不是错误
// 而只是没读到。
func TestReadWaylandUriListImage(t *testing.T) {
	png := makeTestPNG(64, 48)
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imagePath, png, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写入测试文本失败: %v", err)
	}

	t.Run("text/uri-list 命中", func(t *testing.T) {
		setUpFakeWlPaste(t)
		t.Setenv("FAKE_URI_LIST", "file://"+imagePath+"\r\n")
		if got := readWaylandUriListImage(); string(got) != string(png) {
			t.Fatalf("期望 %d 字节，实际 %d 字节", len(png), len(got))
		}
	})
	t.Run("text/uri-list 为空时回退到 gnome-copied-files", func(t *testing.T) {
		setUpFakeWlPaste(t)
		t.Setenv("FAKE_GNOME_LIST", "copy\nfile://"+imagePath+"\n")
		if got := readWaylandUriListImage(); string(got) != string(png) {
			t.Fatalf("期望 %d 字节，实际 %d 字节", len(png), len(got))
		}
	})
	t.Run("URI 指向非图片时返回 nil", func(t *testing.T) {
		setUpFakeWlPaste(t)
		t.Setenv("FAKE_URI_LIST", "file://"+textPath+"\n")
		if got := readWaylandUriListImage(); got != nil {
			t.Fatalf("期望 nil，实际 %d 字节", len(got))
		}
	})
	t.Run("不在 Wayland 会话时返回 nil", func(t *testing.T) {
		t.Setenv("WAYLAND_DISPLAY", "")
		if got := readWaylandUriListImage(); got != nil {
			t.Fatalf("期望 nil，实际 %d 字节", len(got))
		}
	})
}

// TestReadImageFallsBackToWaylandUriList 从 ReadImage 的入口验证第 5 条策略确实被接上：
// 没有 DISPLAY（纯 Wayland 会话）、位图类型全空、只有 URI 列表时，仍应读出文件内容。
// 用 200×200 的图，以便同时过掉 isPlausibleImage 的体积与尺寸下限。
func TestReadImageFallsBackToWaylandUriList(t *testing.T) {
	setUpFakeWlPaste(t)
	t.Setenv("DISPLAY", "")

	png := makeTestPNG(200, 200)
	imagePath := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(imagePath, png, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	t.Setenv("FAKE_URI_LIST", "file://"+imagePath+"\n")

	got, err := ReadImage()
	if err != nil {
		t.Fatalf("期望读取成功，实际报错: %v", err)
	}
	if string(got) != string(png) {
		t.Fatalf("期望 %d 字节，实际 %d 字节", len(png), len(got))
	}
}
