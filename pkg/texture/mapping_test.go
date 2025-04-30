package texture

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestSetup(t *testing.T) {
	if err := os.Chdir("../../"); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
}

func TestMapping(t *testing.T) {
	var assets = NewAsset1x21y("1.21.1")
	image, err := assets.GetPing(-1)
	assert.NoError(t, err)
	assert.NotNil(t, image)
}

func TestGetTexture(t *testing.T) {
	var assets = NewAsset1x21y("1.21.1")
	image, err := assets.GetTexture("minecraft:block/dark_oak_planks")
	assert.NoError(t, err)
	assert.NotNil(t, image)
}

func TestAssetManager(t *testing.T) {
	manager, err := GetAssetManager("1.21.1")
	assert.NoError(t, err)
	image, err := manager.GetPing(200)
	assert.NoError(t, err)
	assert.NotNil(t, image)
}
