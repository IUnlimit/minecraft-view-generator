package tools

import (
	"encoding/base64"
	"image"
	"image/png"

	"github.com/valyala/bytebufferpool"
)

func Image2Base64(img image.Image) (string, error) {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	err := png.Encode(buf, img)
	if err != nil {
		return "", err
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.B), nil
}
