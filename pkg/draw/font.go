package draw

import (
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"os"
)

var Font *opentype.Font

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
