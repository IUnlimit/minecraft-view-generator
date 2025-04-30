package tools

import (
	"github.com/disintegration/imaging"
	"image"
	"image/draw"
	"os"
)

func ReadImage(filePath string) (image.Image, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func Cut(img image.Image, x0 int, y0 int, x1 int, y1 int) (image.Image, error) {
	// 设置裁剪的区域
	rect := image.Rect(x0, y0, x1, y1)
	croppedImg := image.NewRGBA(rect)

	draw.Draw(croppedImg, rect, img, rect.Min, draw.Src)
	return croppedImg, nil
}

// Resize 等比例变化
func Resize(img image.Image, width int, height int) image.Image {
	scale := imaging.Resize(img, width, height, imaging.CatmullRom)
	return scale
}

// Cover 能将一张图覆盖到另一张的指定位置
func Cover(top image.Image, bg image.Image, x0, y0, offsetX, offsetY int) *image.RGBA {
	// 构造一块与 bg 同尺寸（且 origin 与 bg.Bounds().Min 一致）的空白画布
	newImage := image.NewRGBA(bg.Bounds())

	// 把整个背景“原封不动”地复制过来
	//    — 用 newImage.Bounds() 作为目标区域
	//    — 用 bg.Bounds().Min 作为源图起点
	draw.Draw(newImage, newImage.Bounds(), bg, bg.Bounds().Min, draw.Src)

	topBounds := top.Bounds()
	destRect := image.Rect(
		x0+offsetX,
		y0+offsetY,
		x0+offsetX+topBounds.Dx(),
		y0+offsetY+topBounds.Dy(),
	)
	// 把人物贴上去，源点用 top.Bounds().Min
	draw.Draw(newImage, destRect, top, topBounds.Min, draw.Over)
	return newImage
}
