package main

import (
	"context"
	"encoding/base64"
	"io/ioutil"
	"log"

	"github.com/chromedp/chromedp"
)

// TODO
// 在 Go 中用 chromedp 直接驱动 Browserless（WebSocket）并拿到 Canvas DataURL
// 如果你只想拿到 Canvas 的像素数据（Base64），可以用 Go 的 chromedp 库，通过 DevTools Protocol 直接操作页面上的 <canvas>：
func main() {
	// Browserless WebSocket 地址（会自动探测 /json/version）
	dockerURL := "http://localhost:3000"

	// 建立 RemoteAllocator
	allocCtx, cancel := chromedp.NewRemoteAllocator(context.Background(), dockerURL)
	defer cancel()

	// 创建浏览器上下文
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	var dataURL string
	url := "http://127.0.0.1:13300/dashboard?nameTag=IllTamer&skinUrl=dashboard%2Fskins%2Fc359e5045bc74641ac39f318446e7461!!true.png"

	// 执行导航、等待 canvas 出现、取 DataURL
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible("canvas", chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector("canvas").toDataURL("image/png")`, &dataURL),
	)
	if err != nil {
		log.Fatalf("chromedp 运行失败：%v", err)
	}

	// dataURL = "data:image/png;base64,XXXX..."
	// 去掉前缀并解码
	raw := dataURL[len("data:image/png;base64,"):]
	img, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		log.Fatalf("Base64 解码失败：%v", err)
	}

	if err := ioutil.WriteFile("canvas.png", img, 0644); err != nil {
		log.Fatalf("保存失败：%v", err)
	}
	log.Println("已保存 canvas.png")
}
