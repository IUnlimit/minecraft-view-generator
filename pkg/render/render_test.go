package render

import (
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestBuildUrl(t *testing.T) {
	url, err := buildTargetUrl(&Option{
		BaseUrl:  "http://172.17.0.1:4399",
		NameTag:  "IllTamer",
		SkinPath: "skinview3d/skins/c359e5045bc74641ac39f318446e7461!!true.png",
		Width:    300,
		Height:   300,
	})
	assert.NoError(t, err)
	t.Log(url)
}

func TestGetSkin(t *testing.T) {
	skin, err := GetSkin(
		"ws://127.0.0.1:23001",
		15*time.Second,
		&Option{
			BaseUrl:  "http://172.17.0.1:4399",
			NameTag:  "IllTamer",
			SkinPath: "skinview3d/skins/c359e5045bc74641ac39f318446e7461!!true.png",
			Width:    300,
			Height:   300,
		},
	)
	assert.NoError(t, err)
	t.Log(tools.Image2Base64(skin))
}
