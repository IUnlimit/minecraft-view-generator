package draw

import (
	"github.com/IUnlimit/minecraft-view-generator/pkg/component"
	"github.com/fogleman/gg"
	"github.com/nfnt/resize"
	"golang.org/x/image/font"
	"image"
	"image/color"
	"math"
)

type Options struct {
	ShadowOffset float64
	BoldOffset   float64
	OffsetY      float64
}

// NewImageWithBackground create image with background color
func NewImageWithBackground(width float64, height float64, color color.Color, face font.Face) *gg.Context {
	ctx := gg.NewContext(int(math.Ceil(width)), int(math.Ceil(height)))
	ctx.SetColor(color)
	ctx.Clear()
	ctx.SetFontFace(face)
	return ctx
}

// CoveyImage draw one image onto another with resize
func CoveyImage(image image.Image, startX float64, startY float64, height float64, ctx *gg.Context) {
	imgWidth := float64(image.Bounds().Dx())
	imgHeight := float64(image.Bounds().Dy())
	scaleY := height / imgHeight
	scaledImg := resize.Resize(uint(imgWidth*scaleY), uint(height), image, resize.Bicubic)

	ctx.DrawImage(scaledImg, int(startX), int(startY))
}

// PrintChar print chars to image with options
func PrintChar(startX float64, startY float64, c *component.Component, ctx *gg.Context, options *Options) (float64, float64) {
	for _, char := range c.Parse() {
		content := string(char.Content)
		var w float64
		if char.Type == component.Default {
			// draw shadow
			ctx.SetColor(component.DarkGray)
			ctx.DrawString(content, startX+options.ShadowOffset, startY+options.OffsetY+options.ShadowOffset)

			// draw content
			ctx.SetColor(char.Color)
			ctx.DrawString(content, startX, startY+options.OffsetY)

			w, _ = ctx.MeasureString(content)
		} else if char.Type == component.Bold {
			for dx := 0.0; dx <= 1.0; dx++ {
				for dy := 0.0; dy <= 1.0; dy++ {
					// draw shadow
					ctx.SetColor(component.DarkGray)
					ctx.DrawString(content, startX+dx+options.ShadowOffset, startY+options.OffsetY+options.ShadowOffset+dy)

					// draw content
					ctx.SetColor(char.Color)
					ctx.DrawString(content, startX+dx, startY+options.OffsetY+dy)
				}
			}
			w, _ = ctx.MeasureString(content)
			w += options.BoldOffset
		} // TODO StrikeThrough Underline Italic
		startX += w
	}
	return startX, startY
}
