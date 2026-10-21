package font

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot writes a miniature resource tree that carries one provider of every kind the client
// knows: a space provider, a bitmap sheet, a unihex archive and a reference to a second definition.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	grid := make([]string, 16)
	for row := range grid {
		runes := make([]rune, 16)
		for column := range runes {
			runes[column] = rune(row*16 + column)
		}
		grid[row] = string(runes)
	}
	definition := map[string]any{"providers": []any{
		map[string]any{"type": "space", "advances": map[string]any{" ": 4}},
		map[string]any{
			"type": "bitmap", "file": "minecraft:font/ascii.png", "ascent": 7, "height": 8, "chars": grid,
		},
		map[string]any{
			"type": "unihex", "hex_file": "minecraft:font/unifont.zip",
			"size_overrides": []any{map[string]any{"from": "\u3000", "to": "\u9fff", "left": 0, "right": 15}},
		},
		map[string]any{"type": "reference", "id": "minecraft:include/nested"},
	}}
	writeJSON(t, filepath.Join(root, "assets/minecraft/font/default.json"), definition)
	writeJSON(t, filepath.Join(root, "assets/minecraft/font/include/nested.json"), map[string]any{
		"providers": []any{map[string]any{"type": "space", "advances": map[string]any{"\u2007": 5}}},
	})

	writeSheet(t, filepath.Join(root, "assets/minecraft/textures/font/ascii.png"))
	writeUnifont(t, filepath.Join(root, "assets/minecraft/font/unifont.zip"))
	return root
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeSheet paints the glyph of "A" (row four, column one of a sixteen by sixteen grid of eight
// pixel cells) as an open box six columns wide, which is what the advance assertions expect.
func writeSheet(t *testing.T, path string) {
	t.Helper()
	sheet := image.NewRGBA(image.Rect(0, 0, 128, 128))
	originX, originY := 8, 32
	for column := 0; column < 6; column++ {
		sheet.Set(originX+column, originY+1, color.RGBA{255, 255, 255, 255})
	}
	for row := 1; row <= 6; row++ {
		sheet.Set(originX, originY+row, color.RGBA{255, 255, 255, 255})
		sheet.Set(originX+5, originY+row, color.RGBA{255, 255, 255, 255})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, sheet); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeUnifont stores two sixteen pixel wide Unifont glyphs: the narrow "中" of the CJK block, whose
// provider override forces a full width advance, and "℃" outside the override, whose advance is
// computed from the inked columns.
func writeUnifont(t *testing.T, path string) {
	t.Helper()
	rows := func(width int, inked ...string) string {
		body := strings.Join(inked, "")
		for len(body) < unifhexRows*width {
			body += strings.Repeat("0", width)
		}
		return body
	}
	lines := []string{
		"4E2D:" + rows(4, "FF00", "8100"),
		"2103:" + rows(4, strings.Repeat("FF00", unifhexRows)),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("unifont.hex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadFixture(t *testing.T) *Font {
	t.Helper()
	return Load([]string{fixtureRoot(t)})
}

// The space provider is the only source of the vanilla four pixel space, and it carries no pixels.
func TestSpaceProviderUsesVanillaAdvance(t *testing.T) {
	font := loadFixture(t)
	if width := font.Width(" "); width != 4 {
		t.Fatalf("width of the space = %d, want 4", width)
	}
	space := font.Glyph(' ')
	if space.Advance != 4 || space.Width != 0 || space.Height != 0 {
		t.Fatalf("space glyph = %+v, want advance 4 without pixels", space)
	}
}

// A bitmap glyph keeps its left bearing and is advanced by its inked width plus one pixel.
func TestBitmapGlyphIsSlicedFromItsCell(t *testing.T) {
	font := loadFixture(t)
	glyph := font.Glyph('A')
	if glyph.Advance != 7 || glyph.Width != 6 || glyph.Height != 8 || glyph.Ascent != 7 {
		t.Fatalf("glyph = %+v, want advance 7 on a 6x8 cell with ascent 7", glyph)
	}
	for _, point := range []image.Point{{5, 1}, {0, 1}, {5, 6}} {
		if !glyph.Inked(point.X, point.Y) {
			t.Fatalf("pixel %v is not inked in %+v", point, glyph)
		}
	}
	if glyph.Inked(3, 3) {
		t.Fatalf("the empty middle of the box is inked")
	}
	if font.Glyph('B').Width != 0 {
		t.Fatalf("an empty cell has no ink")
	}
}

// Chinese characters are sixteen texel Unifont bitmaps drawn at half scale, so they cover the full
// eight pixel cell regardless of how much ink the bitmap has.
func TestUnihexCJKUsesSizeOverrides(t *testing.T) {
	font := loadFixture(t)
	glyph := font.Glyph('中')
	if glyph.Advance != 9 || glyph.Width != 8 || glyph.Height != 8 {
		t.Fatalf("glyph = %+v, want the full width 8x8 cell with advance 9", glyph)
	}
	if width := font.Width("中中"); width != 18 {
		t.Fatalf("width of two full width glyphs = %d, want 18", width)
	}
	for column := 0; column < 4; column++ {
		if !glyph.Inked(column, 0) {
			t.Fatalf("column %d of the first glyph row is not inked", column)
		}
	}
	if glyph.Inked(7, 0) {
		t.Fatalf("the blank half of the bitmap is inked")
	}
	if glyph.Inked(0, 2) {
		t.Fatalf("rows outside the bitmap are inked")
	}
}

// A half scale texel grid leaves most CJK pixels only partly covered, and the fixture is written so
// that both end pixels of the first row catch the lit last texel of its second texel row: the glyph
// keeps those as greys instead of collapsing to black and white.
func TestUnihexCJKKeepsPartialCoverage(t *testing.T) {
	font := loadFixture(t)
	glyph := font.Glyph('中')
	if coverage := glyph.Coverage(0, 0); coverage != 3 {
		t.Fatalf("coverage of the first pixel = %d, want 3", coverage)
	}
	for column := 1; column <= 2; column++ {
		if coverage := glyph.Coverage(column, 0); coverage != 2 {
			t.Fatalf("coverage of column %d = %d, want 2", column, coverage)
		}
	}
	if coverage := glyph.Coverage(3, 0); coverage != 3 {
		t.Fatalf("coverage of column 3 = %d, want 3", coverage)
	}
	if coverage := glyph.Coverage(0, 1); coverage != 0 {
		t.Fatalf("coverage of the blank second row = %d, want 0", coverage)
	}
}

// A partly covered pixel is drawn as a translucent one, so a scaled CJK glyph keeps the soft edges
// the client has at GUI scale one, and the fully covered pixels stay opaque.
func TestUnihexCJKDrawsSoftEdges(t *testing.T) {
	font := loadFixture(t)
	canvas := image.NewRGBA(image.Rect(0, 0, 16, 16))
	font.Glyph('中').Draw(canvas, 0, 0, 1, color.RGBA{255, 255, 255, 255})
	if alpha := uint8(canvas.RGBAAt(0, 0).A); alpha != 191 {
		t.Fatalf("alpha of a three quarter pixel = %d, want 191", alpha)
	}
	if alpha := uint8(canvas.RGBAAt(1, 0).A); alpha != 127 {
		t.Fatalf("alpha of a half covered pixel = %d, want 127", alpha)
	}
	if alpha := uint8(canvas.RGBAAt(0, 1).A); alpha != 0 {
		t.Fatalf("the blank second row was painted with alpha %d", alpha)
	}
}

// At twice the size one texel lands on one pixel, so the glyph is the sixteen texel Unifont bitmap
// itself rather than the eight pixel average magnified: no greys at all, and exactly one opaque pixel
// per lit texel.
func TestUnihexCJKIsSharpAtTexelScale(t *testing.T) {
	font := loadFixture(t)
	glyph := font.Glyph('中')
	canvas := image.NewRGBA(image.Rect(0, 0, 16, 16))
	glyph.Draw(canvas, 0, 0, 2, color.RGBA{255, 255, 255, 255})
	// The fixture paints the first texel row of U+4E2D as 0xFF00: eight lit texels, then eight blank.
	for column := 0; column < 16; column++ {
		want := uint8(0)
		if column < 8 {
			want = 255
		}
		if alpha := canvas.RGBAAt(column, 0).A; alpha != want {
			t.Fatalf("alpha at %d,0 = %d, want %d", column, alpha, want)
		}
	}
	// The second texel row is 0x8100: the first and the eighth texel, nothing in between.
	if alpha := canvas.RGBAAt(0, 1).A; alpha != 255 {
		t.Fatalf("alpha of the first texel of the second row = %d, want 255", alpha)
	}
	if alpha := canvas.RGBAAt(7, 1).A; alpha != 255 {
		t.Fatalf("alpha of the eighth texel of the second row = %d, want 255", alpha)
	}
	for _, column := range []int{1, 6, 8} {
		if alpha := canvas.RGBAAt(column, 1).A; alpha != 0 {
			t.Fatalf("alpha at %d,1 = %d, want no ink", column, alpha)
		}
	}
	opaque := 0
	for row := 0; row < 16; row++ {
		for column := 0; column < 16; column++ {
			if canvas.RGBAAt(column, row).A == 255 {
				opaque++
			}
		}
	}
	quarters := 0
	for row := 0; row < glyph.Height; row++ {
		for column := 0; column < glyph.Width; column++ {
			quarters += glyph.Coverage(column, row)
		}
	}
	if opaque != quarters {
		t.Fatalf("%d opaque pixels carry %d texels of ink, want exactly one pixel per texel", opaque, quarters)
	}
}

// A scale between two texel multiples is averaged once at the drawn resolution. Eight lit texels at
// three device pixels per font pixel cover twelve opaque pixels; magnifying the half scale average
// instead would repeat the same grey in every three by three block and paint no solid run at all.
func TestUnihexCJKResamplesBetweenTexelScales(t *testing.T) {
	font := loadFixture(t)
	canvas := image.NewRGBA(image.Rect(0, 0, 25, 25))
	font.Glyph('中').Draw(canvas, 0, 0, 3, color.RGBA{255, 255, 255, 255})
	solid := 0
	for column := 0; column < 25; column++ {
		if canvas.RGBAAt(column, 0).A == 255 {
			solid++
		}
	}
	if solid != 12 {
		t.Fatalf("the first texel row carries %d opaque pixels at scale three, want the twelve of eight texels", solid)
	}
	partial := 0
	for column := 0; column < 25; column++ {
		if alpha := canvas.RGBAAt(column, 1).A; alpha > 0 && alpha < 255 {
			partial++
		}
	}
	if partial == 0 {
		t.Fatal("the second row has no partly covered pixel, the glyph was magnified instead of resampled")
	}
}

// Outside a size override the advance follows the inked texels, four of them forming one pixel.
func TestUnihexGlyphTrimsToItsInk(t *testing.T) {
	font := loadFixture(t)
	glyph := font.Glyph('℃')
	if glyph.Width != 4 || glyph.Advance != 5 {
		t.Fatalf("glyph = %+v, want width 4 and advance 5", glyph)
	}
}

// A reference inlines the providers of another definition, which is how include/unifont joins the
// default chain.
func TestReferenceInlinesAnotherDefinition(t *testing.T) {
	font := loadFixture(t)
	if width := font.Width("\u2007"); width != 5 {
		t.Fatalf("width of the referenced space = %d, want 5", width)
	}
}

// A later root overrides an earlier one, so a resource pack may ship its own default definition.
func TestLaterRootOverridesTheDefinition(t *testing.T) {
	base := fixtureRoot(t)
	pack := t.TempDir()
	writeJSON(t, filepath.Join(pack, "assets/minecraft/font/default.json"), map[string]any{
		"providers": []any{map[string]any{"type": "space", "advances": map[string]any{" ": 7}}},
	})
	if width := Load([]string{base, pack}).Width(" "); width != 7 {
		t.Fatalf("width of the packed space = %d, want 7", width)
	}
	if width := Load([]string{base}).Width(" "); width != 4 {
		t.Fatalf("width of the vanilla space = %d, want 4", width)
	}
}

// An uncovered code point still occupies the seven by five missing glyph so that text keeps its
// layout instead of collapsing.
func TestUncoveredCodePointUsesTheMissingGlyph(t *testing.T) {
	font := loadFixture(t)
	glyph := font.Glyph('Ж')
	if glyph.Advance != 6 || glyph.Width != 5 || glyph.Height != 7 {
		t.Fatalf("glyph = %+v, want a 5x7 missing glyph with advance 6", glyph)
	}
	if !glyph.Inked(0, 0) || !glyph.Inked(4, 0) || !glyph.Inked(0, 6) || glyph.Inked(2, 3) {
		t.Fatalf("the missing glyph is not a hollow box")
	}
}

// Without any resource root there is nothing to load and every code point falls back, which keeps
// the renderer's geometry stable instead of dividing by an absent font.
func TestLoadWithoutRootsFallsBack(t *testing.T) {
	font := Load(nil)
	if width := font.Width("abc"); width != 18 {
		t.Fatalf("width = %d, want 18", width)
	}
}

// Drawing magnifies every glyph pixel into a filled square, so a scaled glyph keeps hard edges.
func TestDrawPaintsScaledPixels(t *testing.T) {
	font := loadFixture(t)
	canvas := image.NewRGBA(image.Rect(0, 0, 32, 32))
	font.Glyph('A').Draw(canvas, 10, 4, 2, color.RGBA{255, 0, 0, 255})
	for _, point := range []image.Point{{10, 6}, {20, 6}, {10, 16}} {
		if red, _, _, alpha := canvas.At(point.X, point.Y).RGBA(); alpha == 0 {
			t.Fatalf("pixel %v is empty", point)
			_ = red
		}
	}
	if _, _, _, alpha := canvas.At(22, 6).RGBA(); alpha != 0 {
		t.Fatalf("the pixel past the glyph is painted")
	}
}
