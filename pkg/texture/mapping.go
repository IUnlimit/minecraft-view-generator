package texture

import "strings"

const (
	Root        = "/minecraft/textures"
	ImageSuffix = ".png"
)

type Assets interface {
	GetPing(ping int) Texture
}

func format(args ...string) *Texture {
	var builder strings.Builder
	for _, arg := range args {
		builder.WriteString(arg)
	}
	return NewTexture(builder.String())
}
