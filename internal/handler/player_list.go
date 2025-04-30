package handler

import (
	"fmt"
	"github.com/IUnlimit/minecraft-view-generator/pkg/texture"
	"image"
	"image/color"
	"math"

	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/loader"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/pkg/component"
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	"github.com/fogleman/gg"
	log "github.com/sirupsen/logrus"
	"golang.org/x/image/font"
)

const (
	// FontSize 字体大小
	FontSize = 16
	// OffsetY 文字+图像整体 Y轴偏移量
	OffsetY = 1.0
	// Padding 两边内边距
	Padding = 2
	// LineSpace 行间距, 最好为整数
	LineSpace = 1
)

var (
	// ShadowOffset 阴影偏移
	ShadowOffset = math.Ceil(float64(FontSize)/16) + 1
	// BoldOffset 加粗偏移
	BoldOffset = ShadowOffset + 1

	PlayerRowColor  = &color.RGBA{R: 91, G: 94, B: 91, A: 168}
	BackgroundColor = &color.RGBA{R: 68, G: 71, B: 68, A: 128}

	Options = &draw.Options{
		ShadowOffset: ShadowOffset,
		BoldOffset:   BoldOffset,
		OffsetY:      OffsetY,
	}
)

func GetPlayerList(request *model.PlayerListRequest) (image.Image, error) {
	if len(request.Entry) == 0 {
		return nil, fmt.Errorf("no player data found")
	}

	manager, err := texture.GetAssetManager(request.Version)
	if err != nil {
		return nil, err
	}

	TemplateCtx := gg.NewContext(0, 0)
	face, err := draw.GetFontFace(FontSize, 72)
	if err != nil {
		return nil, err
	}
	TemplateCtx.SetFontFace(face)

	// TODO 每列最多x个，每列最大宽度合并
	//// compute width & height
	//columnLimit := global.Config.Api.PlayerList.SingleColumnLimit

	var maxNameWidth float64
	playerRows := make([]image.Image, 0)
	for _, player := range request.Entry {
		c := component.NewComponent(player.PlayerName)
		nameWidth, _ := c.Compute(TemplateCtx, BoldOffset)
		log.Debugf("nameWidth: %f, maxHeight: -", nameWidth)
		row, err := drawSinglePlayerRow(player, c, nameWidth, face, manager)
		if err != nil {
			log.Errorf("Failed to draw single-player-row, %v", err)
			continue
		}
		maxNameWidth = math.Max(float64(row.Bounds().Dx()), maxNameWidth)
		playerRows = append(playerRows, row)
	}

	config := global.Config.Api.PlayerList
	var maxHeaderWidth float64
	headerComponents := make([]*component.Component, 0)
	for _, header := range config.HeaderText {
		c := component.NewComponent(header)
		headerWidth, _ := c.Compute(TemplateCtx, BoldOffset)
		maxHeaderWidth = math.Max(headerWidth, maxHeaderWidth)
		headerComponents = append(headerComponents, c)
	}

	var maxFooterWidth float64
	footerComponents := make([]*component.Component, 0)
	for _, footer := range config.FooterText {
		c := component.NewComponent(footer)
		footerWidth, _ := c.Compute(TemplateCtx, BoldOffset)
		maxFooterWidth = math.Max(footerWidth, maxFooterWidth)
		footerComponents = append(footerComponents, c)
	}

	// 取最长行为宽
	width := math.Max(maxNameWidth+Padding*2, math.Max(maxHeaderWidth, maxFooterWidth)+Padding*2)
	// 每行都有行间距
	height := (float64(FontSize)+LineSpace)*float64(len(headerComponents)+len(footerComponents)) +
		// 增加阴影偏移 提升美观
		(float64(FontSize)+OffsetY+LineSpace+(ShadowOffset))*float64(len(playerRows)) + ShadowOffset

	ctx := draw.NewImageWithBackground(width, height, BackgroundColor, face)
	_, startY := 0.0, 0.0
	// header
	for _, headerComp := range headerComponents {
		lineWidth, _ := headerComp.Compute(TemplateCtx, BoldOffset)
		headerStartX := math.Floor((width - lineWidth) / 2)
		_, startY = draw.PrintChar(headerStartX, startY+FontSize, headerComp, ctx, Options)
		startY += LineSpace
	}

	startY += ShadowOffset
	// content Padding
	for _, playerRow := range playerRows {
		rowStartX := math.Floor((width - float64(playerRow.Bounds().Dx())) / 2)
		rowOffset := playerRow.Bounds().Dy() - FontSize + LineSpace*2
		log.Debugf("playerRow, dx: %d, dy: %d, rowOffset: %d", playerRow.Bounds().Dx(), playerRow.Bounds().Dy(), rowOffset)
		ctx.DrawImage(playerRow, int(rowStartX), int(startY)+rowOffset)
		startY += FontSize + LineSpace + ShadowOffset
	}

	// footer
	for _, footerComp := range footerComponents {
		lineWidth, _ := footerComp.Compute(TemplateCtx, BoldOffset)
		headerStartX := math.Floor((width - lineWidth) / 2)
		_, startY = draw.PrintChar(headerStartX, startY+FontSize, footerComp, ctx, Options)
		startY += LineSpace
	}

	return ctx.Image(), nil
}

func drawSinglePlayerRow(
	player *model.PlayerListRequestEntry,
	c *component.Component,
	nameWidth float64,
	face font.Face,
	manager texture.AssetsManager,
) (image.Image, error) {
	width := ShadowOffset + nameWidth + 16 + 20 // + head + ping
	height := ShadowOffset + FontSize
	ctx := draw.NewImageWithBackground(width, height, PlayerRowColor, face)

	// TODO 兼容皮肤站
	skin, err := loader.LoadSkinByName(player.PlayerName, player.PlayerUUID, true)
	if err != nil {
		return nil, err
	}

	pingImage, err := manager.GetPing(player.Ping)
	if err != nil {
		return nil, err
	}

	// 3 for Chinese char
	startX, startY := 0.0, height-ShadowOffset-3

	draw.CoveyImage(skin.GetFace(), startX, OffsetY, FontSize, ctx)
	startX += 16 + 1

	startX, startY = draw.PrintChar(startX, startY, c, ctx, Options)

	draw.CoveyImage(pingImage, startX, OffsetY, FontSize, ctx)
	return ctx.Image(), nil
}
