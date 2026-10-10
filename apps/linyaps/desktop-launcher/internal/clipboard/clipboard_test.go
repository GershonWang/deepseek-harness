package clipboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadImageFileFromURIList 覆盖 URI 列表解析：两个通道共用它，X11 与 Wayland
// 因此不会各自长出一份走样的实现。用例同时钉住「哪些输入必须被拒」——扩展名像图片
// 但内容不是、体积超限、路径是目录、指向远端主机，都被拒才不会把坏数据交给渲染层。
func TestReadImageFileFromURIList(t *testing.T) {
	png := makeTestPNG(64, 48)
	dir := t.TempDir()

	imagePath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(imagePath, png, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	// 文件名带空格：真实文管把空格编码成 %20，必须解码后才能命中文件。
	spacedPath := filepath.Join(dir, "带 空格.png")
	if err := os.WriteFile(spacedPath, png, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	// 后缀是 .png 但内容是文本：魔数校验必须拦住它。
	fakePath := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(fakePath, []byte("这不是图片，只是改了后缀"), 0o644); err != nil {
		t.Fatalf("写入伪装图片失败: %v", err)
	}
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("写入文本文件失败: %v", err)
	}
	// 稀疏文件：stat 就能看出超限，无需真的写入 20 MiB。
	hugePath := filepath.Join(dir, "huge.png")
	huge, err := os.Create(hugePath)
	if err != nil {
		t.Fatalf("创建稀疏文件失败: %v", err)
	}
	if err := huge.Truncate(int64(maxImageBytes) + 1); err != nil {
		t.Fatalf("扩展稀疏文件失败: %v", err)
	}
	_ = huge.Close()
	// 名字像图片的目录：必须靠 IsDir 挡住，否则会走到 ReadFile 报错。
	dirPath := filepath.Join(dir, "folder.png")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	encodedSpaced := "file://" + strings.ReplaceAll(spacedPath, " ", "%20")

	cases := []struct {
		name string
		data string
		want []byte
	}{
		{"单行标准 uri-list", "file://" + imagePath + "\n", png},
		{"百分号编码的空格被解码", encodedSpaced + "\n", png},
		{"crlf 行尾", "file://" + imagePath + "\r\n", png},
		{"gnome-copied-files 的 copy 首行被跳过", "copy\nfile://" + imagePath + "\n", png},
		{"注释行与空行被跳过", "# 注释\n\nfile://" + imagePath + "\n", png},
		{"首个候选不可读时继续看下一行", "file://" + dir + "/missing.png\nfile://" + imagePath + "\n", png},
		{"非图片扩展名", "file://" + textPath + "\n", nil},
		{"路径不存在", "file://" + dir + "/missing.png\n", nil},
		{"后缀是图片但内容不是", "file://" + fakePath + "\n", nil},
		{"超过体积上限", "file://" + hugePath + "\n", nil},
		{"路径是目录", "file://" + dirPath + "\n", nil},
		{"远端主机被拒", "file://otherhost" + imagePath + "\n", nil},
		{"没有 file:// 前缀", imagePath + "\n", nil},
		{"空输入", "", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readImageFileFromURIList([]byte(tc.data))
			if tc.want == nil {
				if got != nil {
					t.Fatalf("期望返回 nil，实际 %d 字节", len(got))
				}
				return
			}
			if string(got) != string(tc.want) {
				t.Fatalf("返回内容不符：期望 %d 字节，实际 %d 字节", len(tc.want), len(got))
			}
		})
	}
}
