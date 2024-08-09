package handler

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/pkg/component"
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	"github.com/fogleman/gg"
	"image"
)

const (
	// FontSize 字体大小
	FontSize = 24

	// LineSpace 行间距
	LineSpace = 1.5

	// ShadowOffset 阴影偏移
	ShadowOffset = 2
)

func GetPlayerList(players []any) error {
	fontPath := global.FontsPath + "/" + "Minecraft-AE(Chinese).ttf"
	err := draw.LoadFont(fontPath)
	if err != nil {
		return err
	}

	suggestCtx := gg.NewContext(0, 0)
	face, err := draw.GetFontFace(24, 72)
	if err != nil {
		return err
	}
	suggestCtx.SetFontFace(face)

	// draw sign player row
	playerRows := make([]image.Image, len(players))
	for _, _ = range players {
		name := "§l§a[A]§r§f我是§cIllTamer"
		c := component.NewComponent(name)

		nameWidth := c.ComputeWidth(suggestCtx.MeasureString)
		row := drawSignPlayerRow(c, nameWidth)
		playerRows = append(playerRows, row)
	}

	//// compute width & height
	//columnLimit := global.Config.Api.PlayerList.SingleColumnLimit
	//
	//// draw header
	//// draw content
	//// draw footer
	//
	//width, height := 500, 400
	//startX, startY := 20.0, 40.0
	//lines := []string{"Online Players (1/100) A你6为6好", "aaa"}
	//ctx := gg.NewContext(width, height)
	//
	//do(char, ctx)

	//// 保存为PNG文件
	//err = ctx.SavePNG("./font_test.png")
	//if err != nil {
	//	return err
	//}
	return nil
}

func drawSignPlayerRow(c *component.Component, nameWidth int) image.Image {
	// TODO get head

	// 10x8
	//pingAsset := "/home/illtamer/Code/golang/goland/github/minecraft-view-generator/config/assets/1.21/assets/minecraft/textures/gui/sprites/icon/ping_5.png"

	return nil
}

//func do(char rune, ctx *gg.Context) {
//	err := draw.WithFontFace(24, 72, func(face font.Face) {
//		ctx.SetFontFace(face)
//		// 绘制阴影
//		ctx.SetColor(mcolor.Mapping['8'])
//		ctx.DrawString(string(char), startX+ShadowOffset, startY+ShadowOffset)
//
//		// 绘制正文
//		ctx.SetColor(mcolor.Mapping['a'])
//		ctx.DrawString(string(char), startX, startY)
//	})
//	if err != nil {
//		log.Errorf("%v", err)
//	}
//}
