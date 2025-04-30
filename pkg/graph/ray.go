package graph

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"strings"
)

const modelPath = "/home/illtamer/Code/go/goland/minecraft-view-generator/config/assets/1.21.1/assets/minecraft/models/block/tnt.json"

func Do() error {
	// 设置配置标志以创建 offscreen 上下文
	rl.SetConfigFlags(rl.FlagMsaa4xHint | rl.FlagWindowHidden)

	screenWidth := int32(800)
	screenHeight := int32(600)
	rl.InitWindow(screenWidth, screenHeight, "Minecraft Block Renderer")
	defer rl.CloseWindow()

	model, err := LoadModel(modelPath)
	if err != nil {
		return err
	}
	textures := loadTextures(model)

	// 创建帧缓冲区
	target := rl.LoadRenderTexture(screenWidth, screenHeight)
	defer rl.UnloadRenderTexture(target)

	// 渲染到帧缓冲区
	rl.BeginTextureMode(target)
	rl.ClearBackground(rl.RayWhite)

	// 渲染方块
	renderBlock(model, textures)
	rl.EndTextureMode()

	// 获取渲染的图像
	renderedImage := rl.LoadImageFromTexture(target.Texture)
	// 保存图像为PNG文件
	rl.ExportImage(*renderedImage, "rendered_block.png")

	// 卸载纹理
	for _, texture := range textures {
		rl.UnloadTexture(texture)
	}
	return nil
}

func loadTextures(blockModel *Model) map[string]rl.Texture2D {
	textures := make(map[string]rl.Texture2D)
	for _, path := range blockModel.Textures {
		replace := strings.Replace(path, "minecraft:", "/home/illtamer/Code/go/goland/minecraft-view-generator/config/assets/1.21.1/assets/minecraft/textures/", -1)
		image := rl.LoadImage(replace + ".png")
		textures[path] = rl.LoadTextureFromImage(image)
		rl.UnloadImage(image)
	}
	return textures
}

func renderBlock(block *Model, textures map[string]rl.Texture2D) {
	for _, element := range block.Elements {
		for _, face := range element.Faces {
			texture := textures[block.Textures[face.Texture]]
			// 这里的渲染代码可以更加复杂，处理不同的面，UV，旋转等
			rl.DrawTexture(texture, int32(element.From[0]), int32(element.From[1]), rl.White)
		}
	}
}
