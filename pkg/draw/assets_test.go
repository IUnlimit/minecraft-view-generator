package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/pkg/render"
	"github.com/IUnlimit/minecraft-view-generator/pkg/texture"
	"github.com/fogleman/gg"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
)

func TestSetup(t *testing.T) {
	if err := os.Chdir("../../"); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
}

func TestDrawInventory(t *testing.T) {
	err := LoadFont("./config/fonts/Minecraft.ttf")
	assert.NoError(t, err)

	manager, err := texture.GetAssetManager("1.21.1")
	assert.NoError(t, err)

	inv, err := manager.GetPlayerInventory()
	assert.NoError(t, err)
	skin, err := render.GetSkin(
		"ws://127.0.0.1:23001",
		15*time.Second,
		&render.Option{
			BaseUrl: "http://172.17.0.1:4399",
			//NameTag:  "IllTamer",
			SkinPath: "skinview3d/skins/57876712e6a64cb2ad6419137f209beb!!true.png",
			Width:    113,
			Height:   155,
		},
	)
	assert.NoError(t, err)

	inventory, err := Inventory("IllTamer", inv, skin)
	assert.NoError(t, err)
	_ = gg.SavePNG("./pkg/draw/output/cover.png", inventory)
}
