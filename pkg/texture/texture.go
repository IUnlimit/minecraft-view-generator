package texture

type Texture struct {
	Path string
}

func NewTexture(path string) *Texture {
	return &Texture{Path: path}
}
