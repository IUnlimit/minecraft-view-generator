package draw

import (
	"github.com/fogleman/gg"
	"testing"
)

func TestSkin(t *testing.T) {
	skin, err := NewSkin("ori-skin.png", true)
	if err != nil {
		t.Fatal(err)
	}
	face := skin.GetFace()
	_ = gg.SavePNG("./output/face.png", face)
}
