package draw

import (
	"github.com/fogleman/gg"
	"github.com/mineatar-io/skin-render"
	log "github.com/sirupsen/logrus"
	"image"
	"image/draw"
	"testing"
)

func TestPlayerList(t *testing.T) {
	// icons.png 放置网络图标，用容器颜色，生成MC字体？
}

func TestRenderFace(t *testing.T) {
	rawPlayerSkin, _ := ReadImage("ori-skin.png")
	playerSkin := image.NewNRGBA(rawPlayerSkin.Bounds())
	draw.Draw(playerSkin, rawPlayerSkin.Bounds(), rawPlayerSkin, image.Pt(0, 0), draw.Src)
	face := skin.RenderFace(playerSkin, skin.Options{
		Scale:   1,
		Overlay: true,
		Slim:    true,
	})
	_ = gg.SavePNG("./output/face.png", face)
}

func TestDrawInventory(t *testing.T) {
	inventory, err := Inventory("inventory.png", "skin.png", 2)
	if err != nil {
		log.Fatal(err)
	}
	_ = gg.SavePNG("./output/cover.png", inventory)
}
