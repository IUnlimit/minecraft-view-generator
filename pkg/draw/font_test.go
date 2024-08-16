package draw

import (
	"image/color"
	"testing"

	"github.com/IUnlimit/minecraft-view-generator/pkg/component"
	"github.com/fogleman/gg"
	"github.com/kylelemons/godebug/pretty"
	log "github.com/sirupsen/logrus"
	"golang.org/x/image/font"
)

func TestFont(t *testing.T) {
	err := LoadFont("Minecraft-AE(Chinese).ttf")
	if err != nil {
		log.Fatal(err)
	}
	err = WithFontFace(26, 100, func(face font.Face) {
		width := 500
		// 创建一个新的图形上下文
		context := gg.NewContext(width, 400)

		// 设置字体
		context.SetFontFace(face)

		// 绘制灰色文本
		context.SetColor(color.Gray{Y: 64})
		context.DrawStringWrapped("Online Players (1/100) A你6为6好", 22, 42, 0, 0, float64(width), 1.5, gg.AlignLeft)

		// 绘制绿色文本
		context.SetColor(color.RGBA{R: 0, G: 255, B: 0, A: 255})
		context.DrawStringWrapped("Online Players (1/100) A你6为6好", 20, 40, 0, 0, float64(width), 1.5, gg.AlignLeft)

		// 保存为PNG文件
		err = context.SavePNG("./output/font_test.png")
		if err != nil {
			log.Errorf("保存PNG失败: %v", err)
		}
	})
	if err != nil {
		log.Errorf("%v", err)
	}
}

func TestFontCompute(t *testing.T) {
	component := component.NewComponent("§l§a[A]§r§f我是§cIllTamer")
	suggestCtx := gg.NewContext(0, 0)
	err := LoadFont("/home/illtamer/Code/go/goland/minecraft-view-generator/pkg/draw/Minecraft-AE(Chinese).ttf")
	if err != nil {
		t.Fatal(err)
	}
	face, err := GetFontFace(24, 72)
	if err != nil {
		t.Fatal(err)
	}
	suggestCtx.SetFontFace(face)
	pretty.Print(component.Compute(suggestCtx, 3))
}
