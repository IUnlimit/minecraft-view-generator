package graph

import (
	rl "github.com/gen2brain/raylib-go/raylib"
)

const modelPath = "/home/illtamer/Code/go/goland/minecraft-view-generator/pkg/graph/model_parse.json"

func Do() error {
	screenWidth := int32(160)
	screenHeight := int32(160)
	//distance := int32(20)
	rl.InitWindow(screenWidth, screenHeight, "Minecraft Block Renderer")
	defer rl.CloseWindow()

	// 设置相机
	camera := rl.Camera{
		Position:   rl.NewVector3(0.0, 10.0, 10.0), // 相机的位置
		Target:     rl.NewVector3(0.0, 0.0, 0.0),   // 相机指向的位置
		Up:         rl.NewVector3(0.0, 1.0, 0.0),   // 相机的上方向
		Fovy:       45.0,                           // 视野角度
		Projection: rl.CameraPerspective,           // 透视投影
	}

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
	rl.ClearBackground(rl.Blank)

	//开始3D模式
	rl.BeginMode3D(camera)
	// 渲染方块
	renderBlock(model, textures)
	rl.DrawGrid(10, 1.0)
	// 结束3D模式
	rl.EndMode3D()

	rl.EndTextureMode()

	// 获取渲染的图像
	renderedImage := rl.LoadImageFromTexture(target.Texture)
	// 保存图像为PNG文件
	rl.ExportImage(*renderedImage, "rendered_block.png")

	// 卸载纹理
	for _, t := range textures {
		rl.UnloadTexture(t)
	}
	return nil
}

func loadTextures(blockModel *Model) map[string]rl.Texture2D {
	//manager, _ := texture.GetAssetManager("1.21.1")
	textures := make(map[string]rl.Texture2D)
	for _, path := range blockModel.Textures {
		//t := manager.GetTexture(path)
		image := rl.LoadImage(path)
		textures[path] = rl.LoadTextureFromImage(image)
		rl.UnloadImage(image)
	}
	return textures
}

func renderBlock(block *Model, textures map[string]rl.Texture2D) {
	for _, element := range block.Elements {
		from := rl.NewVector3(element.From[0], element.From[1], element.From[2])
		to := rl.NewVector3(element.To[0], element.To[1], element.To[2])

		// 渲染每个面
		for faceName, face := range element.Faces {
			t := textures[block.Textures[face.Texture]]

			switch faceName {
			//case "down":
			//	drawQuad(t, from.X, from.Y, from.Z, to.X, from.Y, from.Z, to.X, from.Y, to.Z, from.X, from.Y, to.Z)
			case "up":
				drawQuad(t, from.X, to.Y, from.Z, to.X, to.Y, from.Z, to.X, to.Y, to.Z, from.X, to.Y, to.Z)
				//case "north":
				//drawQuad(t, from.X, from.Y, from.Z, to.X, from.Y, from.Z, to.X, to.Y, from.Z, from.X, to.Y, from.Z)
				//case "south":
				//	drawQuad(t, from.X, from.Y, to.Z, to.X, from.Y, to.Z, to.X, to.Y, to.Z, from.X, to.Y, to.Z)
				//case "west":
				//	drawQuad(t, from.X, from.Y, from.Z, from.X, from.Y, to.Z, from.X, to.Y, to.Z, from.X, to.Y, from.Z)
				//case "east":
				//	drawQuad(t, to.X, from.Y, from.Z, to.X, from.Y, to.Z, to.X, to.Y, to.Z, to.X, to.Y, from.Z)
			}
		}
	}
}

//func drawQuad(texture rl.Texture2D, x1, y1, z1, x2, y2, z2, x3, y3, z3, x4, y4, z4 float32) {
//	rl.Begin(rl.Quads)
//	rl.SetTexture(texture.ID)
//	rl.Color4ub(255, 255, 255, 255)
//
//	// Define the normal for the quad
//	normal := calculateNormal(
//		rl.NewVector3(x1, y1, z1),
//		rl.NewVector3(x2, y2, z2),
//		rl.NewVector3(x3, y3, z3),
//	)
//	rl.Normal3f(normal.X, normal.Y, normal.Z)
//
//	// Define the texture coordinates and vertices
//	rl.TexCoord2f(0.0, 0.0)
//	rl.Vertex3f(x1, y1, z1)
//
//	rl.TexCoord2f(1.0, 0.0)
//	rl.Vertex3f(x2, y2, z2)
//
//	rl.TexCoord2f(1.0, 1.0)
//	rl.Vertex3f(x3, y3, z3)
//
//	rl.TexCoord2f(0.0, 1.0)
//	rl.Vertex3f(x4, y4, z4)
//
//	rl.End()
//}
//
//func calculateNormal(v1, v2, v3 rl.Vector3) rl.Vector3 {
//	u := rl.NewVector3(v2.X-v1.X, v2.Y-v1.Y, v2.Z-v1.Z)
//	v := rl.NewVector3(v3.X-v1.X, v3.Y-v1.Y, v3.Z-v1.Z)
//
//	normal := rl.NewVector3(
//		u.Y*v.Z-u.Z*v.Y,
//		u.Z*v.X-u.X*v.Z,
//		u.X*v.Y-u.Y*v.X,
//	)
//
//	// Normalize the normal vector
//	length := float32(math.Sqrt(float64(normal.X*normal.X + normal.Y*normal.Y + normal.Z*normal.Z)))
//	normal.X /= length
//	normal.Y /= length
//	normal.Z /= length
//
//	return normal
//}

func drawQuad(texture rl.Texture2D, x1, y1, z1, x2, y2, z2, x3, y3, z3, x4, y4, z4 float32) {
	x, y, z := float32(-2.0), float32(2.0), float32(0.0)
	width, height, length := float32(2.0), float32(4.0), float32(2.0)

	rl.SetTexture(texture.ID)
	// 开始绘制四边形
	rl.Begin(rl.Quads)
	rl.Color4ub(255, 255, 255, 255)

	// Front Face
	rl.Normal3f(0.0, 0.0, 1.0) // Normal Pointing Towards Viewer
	rl.TexCoord2f(0.0, 0.0)
	rl.Vertex3f(x-width/2, y-height/2, z+length/2) // Bottom Left Of The Texture and Quad
	rl.TexCoord2f(1.0, 0.0)
	rl.Vertex3f(x+width/2, y-height/2, z+length/2) // Bottom Right Of The Texture and Quad
	rl.TexCoord2f(1.0, 1.0)
	rl.Vertex3f(x+width/2, y+height/2, z+length/2) // Top Right Of The Texture and Quad
	rl.TexCoord2f(0.0, 1.0)
	rl.Vertex3f(x-width/2, y+height/2, z+length/2) // Top Left Of The Texture and Quad

	rl.End()
}
