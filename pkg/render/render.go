package render

import (
	"context"
	"errors"
	"fmt"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	log "github.com/sirupsen/logrus"
	"image"
	"net/url"
	"strconv"
	"time"
)

type Option struct {
	BaseUrl  string
	Width    int
	Height   int
	SkinPath string
	CapePath string
	NameTag  string
}

// GetSkin by skinview3d api
func GetSkin(browserLessUrl string, timeout time.Duration, o *Option) (image.Image, error) {
	targetUrl, err := buildTargetUrl(o)
	if err != nil {
		return nil, err
	}
	log.Debugf("render skin target url: %s", targetUrl)

	// Browserless WebSocket 地址（会自动探测 /json/version）
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),                    // 仍然保持 headless
		chromedp.Flag("disable-gpu", false),                // 打开 GPU
		chromedp.Flag("ignore-gpu-blocklist", true),        // 忽略 GPU 黑名单
		chromedp.Flag("enable-webgl", true),                // 开启 WebGL
		chromedp.Flag("disable-software-rasterizer", true), // 禁用软件光栅
	)

	allocCtx, cancel := chromedp.NewExecAllocator(
		context.Background(),
		opts...,
	)
	defer cancel()

	allocCtx, cancel = chromedp.NewRemoteAllocator(
		allocCtx,
		browserLessUrl,
	)
	defer cancel()

	var errs []string
	ctx, cancel := chromedp.NewContext(
		allocCtx,
		chromedp.WithErrorf(func(format string, a ...interface{}) {
			errs = append(errs, fmt.Sprintf(format, a...))
		}))
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	//var dataURL string
	var buf []byte

	// 执行导航、等待 canvas 出现、取 DataURL
	err = chromedp.Run(ctx,
		chromedp.Navigate(targetUrl),
		chromedp.ActionFunc(func(_ctx context.Context) error {
			bg := &cdp.RGBA{R: 0, G: 0, B: 0, A: 0}
			return emulation.SetDefaultBackgroundColorOverride().WithColor(bg).Do(_ctx)
		}),
		chromedp.WaitVisible("canvas", chromedp.ByQuery),
		chromedp.WaitReady("#skin_container", chromedp.ByQuery),
		chromedp.Screenshot("#skin_container", &buf, chromedp.ByQuery, chromedp.NodeVisible),
	)
	for _, e := range errs {
		log.Debugf("chromedp: %s", e)
	}
	if err != nil {
		return nil, err
	}

	i, _, err := tools.BytesToImage(buf)
	if err != nil {
		return nil, err
	}
	if i == nil {
		return nil, errors.New("render failed, is it browserless page load failed")
	}
	return i, nil
}

func buildTargetUrl(o *Option) (string, error) {
	u, err := url.Parse(o.BaseUrl + global.Skinview3dUri)
	if err != nil {
		return "", err
	}

	params := url.Values{} //拼接query参数
	params.Add("nameTag", o.NameTag)
	params.Add("skinUrl", o.SkinPath)
	params.Add("capeUrl", o.CapePath)
	if o.Width != 0 {
		params.Add("width", strconv.Itoa(o.Width))
	}
	if o.Height != 0 {
		params.Add("height", strconv.Itoa(o.Height))
	}
	u.RawQuery = params.Encode()

	return u.String(), nil
}
