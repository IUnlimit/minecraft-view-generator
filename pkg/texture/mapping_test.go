package texture

import "testing"

func TestMapping(t *testing.T) {
	var assets = NewAsset1x21y()
	texture := assets.GetPing(-1)
	t.Log(texture.path)
}
