package draw

import (
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"math"
	"os"
)

var Font *opentype.Font

func DefaultFontOptions(fontSize int, offsetY float64) *FontOptions {
	shadowOffset := math.Ceil(float64(fontSize)/16) + 1
	boldOffset := shadowOffset + 1

	return &FontOptions{
		ShadowOffset: shadowOffset,
		BoldOffset:   boldOffset,
		OffsetY:      offsetY,
	}
}

func LoadFont(path string) error {
	fontBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	parsedFont, err := opentype.Parse(fontBytes)
	if err != nil {
		return err
	}
	Font = parsedFont
	return nil
}

func GetFontFace(fontSize float64, dpi float64) (font.Face, error) {
	face, err := opentype.NewFace(Font, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	return face, nil
}

func WithFontFace(fontSize float64, dpi float64, invoke func(face font.Face)) error {
	// p = size * dpi / 72
	face, err := opentype.NewFace(Font, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return err
	}
	defer face.Close()
	invoke(face)
	return nil
}
