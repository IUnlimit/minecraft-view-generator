package texture

type Texture struct {
	path string
}

func NewTexture(path string) *Texture {
	return &Texture{path: path}
}
