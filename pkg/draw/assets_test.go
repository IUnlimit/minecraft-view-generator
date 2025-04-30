package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/pkg/texture"
	"github.com/fogleman/gg"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestSetup(t *testing.T) {
	if err := os.Chdir("../../"); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
}

func TestDrawInventory(t *testing.T) {
	manager, err := texture.GetAssetManager("1.21.1")
	assert.NoError(t, err)

	inv, err := manager.GetPlayerInventory()
	assert.NoError(t, err)
	inventory, err := Inventory(inv, "./pkg/draw/output-skin.png", 2)
	assert.NoError(t, err)
	_ = gg.SavePNG("./pkg/draw/output/cover.png", inventory)
}
