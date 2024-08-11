package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	"github.com/disintegration/imaging"
	"image"
	"image/draw"
)

type Assets struct {
	Name string
	Path string
	Size int64
}

func Inventory(invPath string, skinPath string, scale int) (image.Image, error) {
	// 背包大小：176 x 166，人物框大小：49 x 70 0.7
	inv, err := Cut(invPath, 0, 0, 176, 166)
	if err != nil {
		return nil, err
	}
	inv = Resize(inv, 176*scale, 166*scale)

	// 人物大小：227 x 447
	skinIn, err := Cut(skinPath, 22, 0, 250, 447)
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
	resizedSkin := Resize(skinIn, 0, 70*scale)

	// 覆盖背包 (26, 8)
	offsetX := (49*scale - resizedSkin.Bounds().Dx()) / 2
	return Cover(resizedSkin, inv, 26*scale, 8*scale, offsetX, -1*scale), nil
}

func Cut(fileName string, x0 int, y0 int, x1 int, y1 int) (image.Image, error) {
	img, err := tools.ReadImage(fileName)
	if err != nil {
		return nil, err
	}

	// 设置裁剪的区域
	rect := image.Rect(x0, y0, x1, y1)
	croppedImg := image.NewRGBA(rect)

	draw.Draw(croppedImg, rect, img, rect.Min, draw.Src)
	return croppedImg, nil
}

// Resize 等比例变化
func Resize(img image.Image, width int, height int) image.Image {
	scale := imaging.Resize(img, width, height, imaging.Lanczos)
	return scale
}

// Cover 能将一张图覆盖到另一张的指定位置
func Cover(top image.Image, bg image.Image, x0 int, y0 int, offsetX int, offsetY int) *image.RGBA {
	newImage := image.NewRGBA(bg.Bounds())
	draw.Draw(newImage, bg.Bounds(), bg, image.Point{}, draw.Over)

	topBounds := top.Bounds()
	destRect := image.Rect(x0+offsetX, y0+offsetY, x0+topBounds.Dx()+offsetX, y0+topBounds.Dy()+offsetY)
	draw.Draw(newImage, destRect, top, image.Point{}, draw.Over)
	return newImage
}
