package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	"github.com/disintegration/imaging"
	"image"
)

type Assets struct {
	Name string
	Path string
	Size int64
}

func Inventory(inv image.Image, skinPath string, scale int) (image.Image, error) {
	// 背包大小：352 x 332，人物框大小：49 x 70 0.7
	// 人物大小：227 x 447
	skinImg, err := tools.ReadImage(skinPath)
	if err != nil {
		return nil, err
	}
	skinIn, err := tools.Cut(skinImg, 22, 0, 250, 447)
	//// 人物大小：444 x 848
	//skinIn, err := Cut("skin-15.png", 36, 0, 481, 848)
	//// 人物大小：862 x 1678
	//skinIn, err := Cut("skin-30.png", 87, 0, 950, 1678)
	if err != nil {
		return nil, err
	}
	// 差别不大 加点锐化
	skinIn = imaging.Sharpen(skinIn, 1)
	// 重绘人物
	resizedSkin := tools.Resize(skinIn, 0, 70*scale)

	// 覆盖背包 (26, 8)
	offsetX := (49*scale - resizedSkin.Bounds().Dx()) / 2
	return tools.Cover(resizedSkin, inv, 26*scale, 8*scale, offsetX, -1*scale), nil
}
