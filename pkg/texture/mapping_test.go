package texture

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMapping(t *testing.T) {
	var assets = NewAsset1x21y("1.21")
	texture := assets.GetPing(-1)
	t.Log(texture.Path)
}

func TestGetTexture(t *testing.T) {
	var assets = NewAsset1x21y("1.21.1")
	texture := assets.GetTexture("minecraft:block/oak_planks")
	t.Log(texture.Path)
}

func TestAssetManager(t *testing.T) {
	manager, err := GetAssetManager("1.21.1")
	assert.NoError(t, err)
	texture := manager.GetPing(200)
	t.Log(texture.Path)
}
