package draw

import (
	"github.com/fogleman/gg"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	log "github.com/sirupsen/logrus"
	"image"
	"image/color"
	"io/ioutil"
	"testing"
)

func TestFont(t *testing.T) {
	//读取字体数据  http://fonts.mobanwang.com/201503/12436.html
	//fontBytes, err := ioutil.ReadFile("Minecraft-Bold.otf")
	fontBytes, err := ioutil.ReadFile("Monocraft.ttf")
	if err != nil {
		log.Println(err)
	}
	//载入字体数据
	font, err := freetype.ParseFont(fontBytes)
	if err != nil {
		log.Println("载入字体失败", err)
	}
	face := truetype.NewFace(font, &truetype.Options{
		Size: 26,
		DPI:  100,
	})

	context := gg.NewContext(2000, 200)

	img := image.NewRGBA(image.Rect(0, 0, srcWidth, srcHeight))

	c := freetype.NewContext()
	c.SetClip(img.Bounds())
	//设置输出的图片
	c.SetDst(img)

	c.SetSrc(image.NewUniform(color.Gray{Y: 64}))
	pt := freetype.Pt(22, 42)
	_, err = c.DrawString("Online Players (1/100)", pt)
	if err != nil {
		log.Fatal(err)
	}

	c.SetSrc(image.NewUniform(color.RGBA{R: 0, G: 255, B: 0, A: 255}))
	//设置字体的位置
	pt = freetype.Pt(20, 40)
	_, err = c.DrawString("Online Players (1/100)你好", pt)
	if err != nil {
		log.Fatal(err)
	}

	_ = context.SavePNG("./output/font_test.png")
}
