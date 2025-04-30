package texture

import "testing"

func TestMapping(t *testing.T) {
	var assets = NewAsset1x21y("1.21")
	texture := assets.GetPing(-1)
	t.Log(texture.path)
}

func TestGetTexture(t *testing.T) {
	var assets = NewAsset1x21y("1.21.1")
	texture := assets.GetTexture("minecraft:block/oak_planks")
	t.Log(texture.path)
}
