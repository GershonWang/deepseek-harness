package tray

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// pixelAt 读取位图中的一个像素，返回 {A, R, G, B}。
// @param pixmap - 位图。
// @param x - 横坐标。
// @param y - 纵坐标。
// @returns 该像素的四个分量。
func pixelAt(pixmap Pixmap, x, y int) [4]byte {
	offset := (y*int(pixmap.Width) + x) * 4
	return [4]byte{pixmap.Data[offset], pixmap.Data[offset+1], pixmap.Data[offset+2], pixmap.Data[offset+3]}
}

// TestToPixmapIsBigEndianArgb32 覆盖字节序：SNI 要求 ARGB32 且为大端，
// 顺序写错时图标颜色会整体错位（例如蓝色图标显示成红色）。
func TestToPixmapIsBigEndianArgb32(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0x44})

	got := toPixmap(img)
	if got.Width != 1 || got.Height != 1 {
		t.Fatalf("尺寸 = %dx%d，期望 1x1", got.Width, got.Height)
	}
	if want := [4]byte{0x44, 0x11, 0x22, 0x33}; pixelAt(got, 0, 0) != want {
		t.Fatalf("像素 = %v，期望 %v（A,R,G,B）", pixelAt(got, 0, 0), want)
	}
}

// TestPixmapsFallBackWhenIconsMissing 覆盖图标缺失：开发态或异常安装下前缀为空，
// 仍须产出可用位图，否则托盘项可能整项不显示。
func TestPixmapsFallBackWhenIconsMissing(t *testing.T) {
	got := pixmaps(t.TempDir(), switchboard.ModeShell)
	if len(got) != len(iconSizes) {
		t.Fatalf("位图数量 = %d，期望 %d", len(got), len(iconSizes))
	}
	for i, pixmap := range got {
		if int(pixmap.Width) != iconSizes[i] || int(pixmap.Height) != iconSizes[i] {
			t.Fatalf("第 %d 张尺寸 = %dx%d，期望 %d", i, pixmap.Width, pixmap.Height, iconSizes[i])
		}
		if len(pixmap.Data) != iconSizes[i]*iconSizes[i]*4 {
			t.Fatalf("第 %d 张数据长度 = %d", i, len(pixmap.Data))
		}
	}
}

// TestPixmapsUsePackagedIconAndBadge 覆盖真实安装布局：从
// <PREFIX>/share/icons/hicolor/<N>x<N>/apps/dsh-desktop.png 读品牌图标，
// 并在右下角叠上形态角标。
func TestPixmapsUsePackagedIconAndBadge(t *testing.T) {
	prefix := t.TempDir()
	const size = 22
	dir := filepath.Join(prefix, "share", "icons", "hicolor", "22x22", "apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("准备图标目录失败: %v", err)
	}
	base := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			base.SetNRGBA(x, y, color.NRGBA{R: 0xff, A: 0xff})
		}
	}
	file, err := os.Create(filepath.Join(dir, "dsh-desktop.png"))
	if err != nil {
		t.Fatalf("创建图标失败: %v", err)
	}
	if err := png.Encode(file, base); err != nil {
		_ = file.Close()
		t.Fatalf("编码图标失败: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("关闭图标失败: %v", err)
	}

	var target Pixmap
	for _, pixmap := range pixmaps(prefix, switchboard.ModeOfficial) {
		if int(pixmap.Width) == size {
			target = pixmap
		}
	}
	if target.Data == nil {
		t.Fatal("未找到 22x22 的位图")
	}
	// 左上角应还是随包图标的红色；右下角应是官方版的蓝色角标。
	if got := pixelAt(target, 1, 1); got != [4]byte{0xff, 0xff, 0x00, 0x00} {
		t.Fatalf("左上角像素 = %v，期望随包图标的红色", got)
	}
	want := badgeColor(switchboard.ModeOfficial)
	radius := size / 5
	cx, cy := size-radius-1, size-radius-1
	if got := pixelAt(target, cx, cy); got != [4]byte{want.A, want.R, want.G, want.B} {
		t.Fatalf("角标像素 = %v，期望 %v", got, [4]byte{want.A, want.R, want.G, want.B})
	}
}

// TestBadgeColorIsDistinctPerMode 覆盖角标可区分：两种形态与「无客户端」三种状态
// 必须给出不同颜色，否则角标等于没有信息。
func TestBadgeColorIsDistinctPerMode(t *testing.T) {
	seen := map[color.RGBA]switchboard.Mode{}
	for _, mode := range []switchboard.Mode{switchboard.ModeShell, switchboard.ModeOfficial, ""} {
		c := badgeColor(mode)
		if other, dup := seen[c]; dup {
			t.Fatalf("形态 %q 与 %q 的角标颜色相同：%v", mode, other, c)
		}
		seen[c] = mode
	}
}
