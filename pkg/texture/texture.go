package texture

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	"image"
)

type Texture struct {
	Path  string
	Image image.Image
}

func NewTexture(path string) *Texture {
	return &Texture{Path: path}
}

func (t *Texture) load() error {
	data, err := tools.ReadImage(t.Path)
	if err != nil {
		return err
	}
	t.Image = data
	return nil
}
