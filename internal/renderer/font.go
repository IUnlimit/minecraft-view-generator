package renderer

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"

	"github.com/illtamer/minecraft-view-generator2/internal/font"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// The fallback renderer draws its text with the same font pipeline as the Java backend: the
// Minecraft provider chain of the resource context (assets/minecraft/font/default.json), so a
// Chinese label is measured with the Unifont advance of nine pixels and drawn from the same 16x16
// bitmap the game uses, instead of a hand written glyph table that only knew ASCII.

var (
	fontMu    sync.Mutex
	fontCache = map[string]*font.Font{}
)

// fontFor returns the Minecraft font of a resource context, loaded once per set of pack roots.
func fontFor(resource resources.Context) *font.Font {
	roots := fontRoots(resource)
	// The revision is part of the key because a pack replaced under the same file name keeps its
	// extracted path: the roots alone would answer with the glyphs of the previous archive.
	key := strings.Join(roots, "\x00") + "\x01" + strconv.FormatInt(resource.PackRevision, 10)
	fontMu.Lock()
	defer fontMu.Unlock()
	if cached, ok := fontCache[key]; ok {
		return cached
	}
	loaded := font.Load(roots)
	fontCache[key] = loaded
	return loaded
}

// fontRoots mirrors the pack order of the Java resource stack: the context root, the vanilla tree,
// the addon and finally the custom packs, each overriding the one before it.
func fontRoots(resource resources.Context) []string {
	candidates := make([]string, 0, 3+len(resource.PackRoots))
	candidates = append(candidates, resource.Root, resource.OfficialRoot, resource.AddonRoot)
	candidates = append(candidates, resource.PackRoots...)
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" {
			roots = append(roots, candidate)
		}
	}
	return roots
}

// drawText draws one line of text at the vanilla label geometry: y is the top of the eight pixel
// text cell and the baseline sits font.Ascent scaled pixels below it, exactly the coordinate
// GuiGraphics.drawString takes. Text that would cross maxWidth is left out entirely, and the drawn
// width is returned.
func drawText(img *image.RGBA, f *font.Font, x, y int, text string, scale int, foreground color.Color, maxWidth int) int {
	if scale < 1 {
		scale = 1
	}
	ink := color.RGBAModel.Convert(foreground).(color.RGBA)
	baseline := y + font.Ascent*scale
	cursor := x
	for _, r := range text {
		glyph := f.Glyph(r)
		advance := glyph.Advance * scale
		if maxWidth > 0 && cursor+advance > x+maxWidth {
			break
		}
		glyph.Draw(img, cursor, baseline-glyph.Ascent*scale, scale, ink)
		cursor += advance
	}
	return cursor - x
}

func textWidth(f *font.Font, text string, scale int) int {
	if scale < 1 {
		scale = 1
	}
	return f.Width(text) * scale
}

func drawCenteredText(img *image.RGBA, f *font.Font, y int, text string, scale int, foreground color.Color) {
	width := textWidth(f, text, scale)
	drawText(img, f, (img.Bounds().Dx()-width)/2, y, text, scale, foreground, img.Bounds().Dx())
}
