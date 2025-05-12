package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	"github.com/IUnlimit/minecraft-view-generator/pkg/component"
	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	log "github.com/sirupsen/logrus"
	"image"
	"image/color"
)

type Assets struct {
	Name string
	Path string
	Size int64
}

var (
	// 背包大小：352 x 332
	InvWidth, InvHeight = 352, 332
	// 人物框大小：100 x 140
	PlayerWidth, PlayerHeight = 100, 140
	// 人物框起点 (52, 16)
	PlayerStartX, PlayerStartY = 52, 16
	BackgroundColor            = &color.RGBA{R: 184, G: 184, B: 184, A: 255}
	fontSize                   = 8
)

func Inventory(name string, inv image.Image, skin image.Image) (image.Image, error) {
	// 差别不大 加点锐化
	skin = imaging.Sharpen(skin, 0.5)
	// 覆盖背包 start (52, 16)
	inv = tools.Cover(skin, inv, PlayerStartX, PlayerStartY, -10, 3)
	invCtx := gg.NewContextForImage(inv)
	// 平移 -Bounds.Min，使得内部坐标原点指向真正的图像左上角
	invCtx.Translate(float64(inv.Bounds().Min.X), float64(inv.Bounds().Min.Y))
	log.Debugf("InvCtx bounds min (x: %d, y: %d)", inv.Bounds().Min.X, inv.Bounds().Min.Y)

	// name tag
	if name != "" {
		fontOptions := DefaultFontOptions(fontSize, float64(fontSize)-1)
		// 字号缩小一倍,阴影间距减小 (default 2)
		fontOptions.ShadowOffset -= 1
		c := component.NewComponent(name)
		face, err := GetFontFace(float64(fontSize), 72)
		if err != nil {
			return nil, err
		}
		invCtx.SetFontFace(face)

		fontWidth, _ := c.Compute(invCtx, fontOptions.BoldOffset)
		// name tag start pos
		startX, startY :=
			float64(PlayerStartX)+(float64(PlayerWidth)-fontWidth)/2-2,
			float64(PlayerStartY)+2
		//bg := NewImageWithBackground(fontWidth, float64(fontSize), BackgroundColor, face).Image()
		//invCtx.DrawImage(bg, int(startX), int(startY))
		startX, startY = PrintChar(startX, startY, c, invCtx, fontOptions)
	}

	return invCtx.Image(), nil
}
