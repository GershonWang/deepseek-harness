package tray

import (
	"image"
	"image/color"
	_ "image/png" // 随包图标是 PNG，注册解码器
	"os"
	"path/filepath"
	"strconv"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// iconSizes 是随包提供的图标尺寸，也是托盘项上报的位图尺寸。
//
// 三个尺寸都上报而不是只给一个：不同宿主面板按自己的尺寸取最接近的一张，只给一张
// 会让某些面板缩放后发虚。
var iconSizes = []int{16, 22, 24}

// badgeColor 给出形态角标的颜色：薄壳版绿色、官方版蓝色，没有客户端时灰色。
//
// 用颜色而不是字母：20 像素级别的图标里文字不可读，而托盘项的 ToolTip 与菜单已经
// 写明形态名，角标只需要「一眼看出和刚才不一样」。
// @param mode - 当前形态；空串表示没有客户端在运行。
// @returns 角标颜色。
func badgeColor(mode switchboard.Mode) color.RGBA {
	switch mode {
	case switchboard.ModeShell:
		return color.RGBA{R: 0x2e, G: 0xc4, B: 0x6b, A: 0xff}
	case switchboard.ModeOfficial:
		return color.RGBA{R: 0x1e, G: 0x88, B: 0xe5, A: 0xff}
	default:
		return color.RGBA{R: 0x8a, G: 0x8a, B: 0x8a, A: 0xff}
	}
}

// pixmaps 生成托盘图标位图：随包品牌图标叠一个形态角标。
// @param prefix - 安装前缀（<PREFIX>）；为空或图标缺失时回退到内置图形。
// @param mode - 当前形态。
// @returns 三个尺寸的 ARGB32 位图。
func pixmaps(prefix string, mode switchboard.Mode) []Pixmap {
	out := make([]Pixmap, 0, len(iconSizes))
	for _, size := range iconSizes {
		out = append(out, toPixmap(withBadge(baseIcon(prefix, size), mode)))
	}
	return out
}

// baseIcon 读取随包的品牌图标。
//
// 路径与 linglong.yaml 安装图标的位置一致；读不到就画一个中性圆点——托盘项没有图标
// 时宿主可能整项都不显示，回退图形保证入口始终可见。
// @param prefix - 安装前缀。
// @param size - 目标边长。
// @returns 该尺寸的基础图标。
func baseIcon(prefix string, size int) image.Image {
	if prefix != "" {
		path := filepath.Join(prefix, "share", "icons", "hicolor",
			strconv.Itoa(size)+"x"+strconv.Itoa(size), "apps", "dsh-desktop-dual.png")
		if file, err := os.Open(path); err == nil {
			defer func() { _ = file.Close() }()
			if img, _, err := image.Decode(file); err == nil {
				return img
			}
		}
	}
	return fallbackIcon(size)
}

// fallbackIcon 画一个中性圆点，用于随包图标缺失的开发态或异常安装。
// @param size - 目标边长。
// @returns 生成的图标。
func fallbackIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillCircle(img, size/2, size/2, size/2-1, color.RGBA{R: 0x4a, G: 0x4a, B: 0x4a, A: 0xff})
	return img
}

// withBadge 在基础图标右下角画形态角标。
//
// 角标带一圈对比色描边：托盘底色随主题变化，纯色角标在浅色与深色主题下会有一侧看不清。
// @param base - 基础图标。
// @param mode - 当前形态。
// @returns 叠好角标的 RGBA 图标。
func withBadge(base image.Image, mode switchboard.Mode) *image.RGBA {
	bounds := base.Bounds()
	size := bounds.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	drawBase(dst, base)
	radius := size / 5
	if radius < 2 {
		radius = 2
	}
	cx, cy := size-radius-1, size-radius-1
	fillCircle(dst, cx, cy, radius, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	fillCircle(dst, cx, cy, radius-1, badgeColor(mode))
	return dst
}

// drawBase 把基础图标铺进目标画布；尺寸不一致时按左上角对齐复制，不缩放。
// @param dst - 目标画布。
// @param base - 基础图标。
func drawBase(dst *image.RGBA, base image.Image) {
	bounds := base.Bounds()
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			dst.Set(x, y, base.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
}

// fillCircle 在画布上画一个实心圆（含半透明混合），圆心与半径以像素计。
// @param dst - 目标画布。
// @param cx - 圆心横坐标。
// @param cy - 圆心纵坐标。
// @param radius - 半径。
// @param c - 填充色。
func fillCircle(dst *image.RGBA, cx, cy, radius int, c color.RGBA) {
	if radius <= 0 {
		return
	}
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy > radius*radius {
				continue
			}
			if !(image.Point{X: x, Y: y}).In(dst.Bounds()) {
				continue
			}
			dst.Set(x, y, c)
		}
	}
}

// toPixmap 把图像转成 SNI 要求的大端 ARGB32 位图。
//
// 用 NRGBA 而不是 RGBA()：后者给的是预乘值，半透明像素（图标边缘的抗锯齿）会偏暗；
// SNI 的 ARGB32 是非预乘的。
// @param img - 源图像。
// @returns 位图。
func toPixmap(img image.Image) Pixmap {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	data := make([]byte, 0, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			data = append(data, c.A, c.R, c.G, c.B)
		}
	}
	return Pixmap{Width: int32(width), Height: int32(height), Data: data}
}
