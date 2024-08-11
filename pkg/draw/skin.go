package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	"github.com/mineatar-io/skin-render"
	"image"
	"image/draw"
)

type Skin struct {
	File *image.NRGBA
}

func NewSkin(filePath string) (*Skin, error) {
	file, err := tools.ReadImage(filePath)
	if err != nil {
		return nil, err
	}
	playerSkin := image.NewNRGBA(file.Bounds())
	draw.Draw(playerSkin, file.Bounds(), file, image.Pt(0, 0), draw.Src)
	return &Skin{
		File: playerSkin,
	}, nil
}

// GetFace 1 scale for 8 px
func (s *Skin) GetFace() *image.NRGBA {
	return skin.RenderFace(s.File, skin.Options{
		Scale:   2,
		Overlay: true,
		Slim:    true,
	})
}
