// Package font implements the Minecraft font pipeline: the provider chain the client loads from
// assets/minecraft/font/default.json, with the metrics the game measures every label with.
//
// The chain is the one the vanilla definition declares: the space advances, the bitmap sheets of
// include/default (ascii, accented, non latin european) and finally include/unifont, whose unihex
// provider draws the 16x16 Unifont bitmaps that carry Chinese. Providers are consulted in
// declaration order and the first hit wins, so a Chinese character can never fall through to a
// Latin sheet.
//
// Metrics are the client's. Every provider sits on the seven pixel ascent, a bitmap glyph is eight
// (or, for the accented sheet, twelve) pixels tall starting at its own ascent, and a unihex glyph is
// sixteen texels drawn at half scale: it fills eight pixels of the eight pixel cell with its top on
// the ascent line, one pixel below the baseline. Advances are the per glyph values the client
// computes — the last inked column plus one for a bitmap glyph, half the inked column count plus one
// for a unihex glyph.
//
// Colour is the client's too: every drawn pixel is the colour the sheet ships times the text colour.
// The vanilla sheets are pure white and therefore answer the text colour, while an icon a resource
// pack ships with colours of its own keeps them, dimmed by a coloured label.
package font

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The metrics every text surface uses. Ascent is the client's fwq.a line ascent; LineHeight is the
// nine pixel line the tooltip and chat layouts advance by.
const (
	Ascent     = 7
	LineHeight = 9
)

const (
	definitionPath     = "assets/minecraft/font/default.json"
	maxReferenceDepth  = 8
	missingAdvance     = 6
	missingWidth       = 5
	missingHeight      = 7
	unihexCell         = 8
	unihexDefaultLeft  = 0
	unihexDefaultRight = 15
	// defaultGlyphHeight is the height the client draws a bitmap provider at when the definition
	// declares none, whatever resolution the sheet has. An HD font pack therefore has to spell its
	// own height out to stay sharp, and a pack that ships two texels per font pixel declares half
	// the cell height here.
	defaultGlyphHeight = 8
)

// Glyph is one drawable glyph: its advance, the baseline offset of its top edge and its per pixel
// coverage. Coverage is measured in quarters, from 0 (empty) to 4 (fully inked). The bitmap sheets
// of the vanilla font only ever answer 0 or 4, while a glyph drawn from a native texel grid — a
// sixteen texel Unifont bitmap at half scale, or a bitmap sheet that ships more texels than its
// declared height — is resampled: a pixel that only covers one or two of its four texels is
// therefore semi transparent, which is the soft edge the game has at GUI scale one.
//
// Such a glyph also keeps its native texel grid. The averaged cell is what the client shows at half
// scale, but flattening sixteen texels into eight pixels throws away half the resolution, and
// magnifying that average is what turned Chinese drawn at twice the size into blocks of grey.
// Drawing from the grid instead puts one texel on one device pixel at scale two.
type Glyph struct {
	Advance  int
	Ascent   int
	Width    int
	Height   int
	coverage []uint8
	// ink is the colour of each font pixel as 0xRRGGBB, or nil when the glyph paints its whole box
	// white — every vanilla sheet and every unihex glyph. A drawn pixel is that colour times the text
	// colour, which is how the client draws a sheet that ships colours of its own: a white pixel takes
	// the text colour, a coloured one keeps its hue at the brightness of that colour.
	ink []uint32
	// texels is the native texel grid as 0xAARRGGBB, row major, or nil when the glyph has no
	// resolution above its own pixels.
	texels []uint32
	// texelWidth and texelHeight describe that grid.
	texelWidth  int
	texelHeight int
	// texelScale is texels per font pixel: 2 for a unihex bitmap, cellHeight/height for a bitmap
	// sheet whose cell is not as tall as the height it declares, 0 when there is no grid.
	texelScale float64
}

// Coverage is how much of the pixel at column, row is inked, in quarters from 0 to 4.
func (g *Glyph) Coverage(column, row int) int {
	if row < 0 || row >= g.Height || column < 0 || column >= g.Width {
		return 0
	}
	index := row*g.Width + column
	if index < 0 || index >= len(g.coverage) {
		return 0
	}
	return int(g.coverage[index])
}

// Inked reports whether a pixel is at least half covered: the pixels a half scale downsampling turns
// into a solid one.
func (g *Glyph) Inked(column, row int) bool {
	return g.Coverage(column, row) >= 2
}

// String keeps diagnostics readable: a glyph carries up to a few hundred coverage values, which
// would bury the assertion that failed.
func (g *Glyph) String() string {
	return fmt.Sprintf("%dx%d advance %d ascent %d", g.Width, g.Height, g.Advance, g.Ascent)
}

// Draw paints the glyph with its top edge at top and its left bearing at x, one font pixel per
// scale device pixels. A glyph that carries a texel grid and is drawn at its own texel scale or
// larger is resampled from that grid, so twice the size is the sixteen texel bitmap itself; every
// other glyph is composited from its averaged coverage, which is the half scale soft edge the client
// has at GUI scale one. A glyph that carries colours of its own is drawn as its colour times the text
// colour, so a pack's coloured icon keeps its tint instead of turning into a solid block. Every write
// goes through draw.Over, so text stays readable on top of a panel.
func (g *Glyph) Draw(dst *image.RGBA, x, top, scale int, foreground color.RGBA) {
	if dst == nil {
		return
	}
	if scale < 1 {
		scale = 1
	}
	if len(g.texels) > 0 && float64(scale) >= g.texelScale {
		g.drawTexels(dst, x, top, scale, foreground)
		return
	}
	bounds := dst.Bounds()
	for row := 0; row < g.Height; row++ {
		for column := 0; column < g.Width; column++ {
			coverage := g.Coverage(column, row)
			if coverage == 0 {
				continue
			}
			ink := modulate(g.inkAt(column, row), foreground)
			if coverage < 4 {
				ink.A = uint8(int(foreground.A) * coverage / 4)
			}
			rect := image.Rect(x+column*scale, top+row*scale, x+(column+1)*scale, top+(row+1)*scale).Intersect(bounds)
			if rect.Empty() {
				continue
			}
			draw.Draw(dst, rect, &image.Uniform{C: ink}, image.Point{}, draw.Over)
		}
	}
}

// inkAt answers the colour of one font pixel, white when the glyph paints none: a blank pixel and
// the column a bold glyph widens past its own box both fall back to the text colour.
func (g *Glyph) inkAt(column, row int) uint32 {
	if g.ink == nil || row < 0 || row >= g.Height {
		return 0xFFFFFF
	}
	source := column
	if column < 0 || column >= g.Width || g.coverage[row*g.Width+column] == 0 {
		source = column - 1
	}
	if source < 0 || source >= g.Width {
		return 0xFFFFFF
	}
	if value := g.ink[row*g.Width+source]; value != 0 {
		return value
	}
	return 0xFFFFFF
}

// modulate multiplies the colour of one glyph pixel into the text colour, which is how the client
// draws a sheet that ships colours of its own: a white pixel takes the text colour as it is, and a
// coloured one keeps its hue at the brightness the text colour allows.
func modulate(texel uint32, foreground color.RGBA) color.RGBA {
	if texel == 0xFFFFFF {
		return foreground
	}
	foreground.R = uint8(uint32(foreground.R) * (texel >> 16 & 0xFF) / 255)
	foreground.G = uint8(uint32(foreground.G) * (texel >> 8 & 0xFF) / 255)
	foreground.B = uint8(uint32(foreground.B) * (texel & 0xFF) / 255)
	return foreground
}

// drawTexels resamples the native texel grid at the drawn resolution: every device pixel answers the
// share of its own area that lit texels cover, and — when the glyph carries colours of its own — the
// colour of that share as well. At scale == texelScale each texel lands on exactly one pixel, so the
// glyph is the bitmap the font ships; a scale between two texel multiples is averaged once, at the
// resolution it is painted at, instead of magnifying the average taken at font pixel resolution.
func (g *Glyph) drawTexels(dst *image.RGBA, x, top, scale int, foreground color.RGBA) {
	span := g.texelScale / float64(scale)
	area := span * span
	tinted := g.ink != nil
	bounds := dst.Bounds()
	width, height := g.Width*scale, g.Height*scale
	for row := 0; row < height; row++ {
		rowLow := float64(row) * span
		rowHigh := rowLow + span
		for column := 0; column < width; column++ {
			columnLow := float64(column) * span
			columnHigh := columnLow + span
			lit := 0.0
			var red, green, blue float64
			for texelRow := max(0, int(math.Floor(rowLow))); texelRow < min(g.texelHeight, int(math.Ceil(rowHigh))); texelRow++ {
				rowWeight := math.Min(rowHigh, float64(texelRow+1)) - math.Max(rowLow, float64(texelRow))
				if rowWeight <= 0 {
					continue
				}
				base := texelRow * g.texelWidth
				for texelColumn := max(0, int(math.Floor(columnLow))); texelColumn < min(g.texelWidth, int(math.Ceil(columnHigh))); texelColumn++ {
					columnWeight := math.Min(columnHigh, float64(texelColumn+1)) - math.Max(columnLow, float64(texelColumn))
					if columnWeight <= 0 {
						continue
					}
					texel := g.texels[base+texelColumn]
					weight := columnWeight * rowWeight * float64(texel>>24) / 255
					if weight <= 0 {
						continue
					}
					lit += weight
					if tinted {
						red += weight * float64(texel>>16&0xFF)
						green += weight * float64(texel>>8&0xFF)
						blue += weight * float64(texel&0xFF)
					}
				}
			}
			if lit <= 0 {
				continue
			}
			alpha := int(math.Round(lit / area * 255))
			if alpha > 255 {
				alpha = 255
			}
			ink := foreground
			if tinted {
				texel := uint32(math.Round(red/lit))<<16 | uint32(math.Round(green/lit))<<8 | uint32(math.Round(blue/lit))
				ink = modulate(texel, foreground)
			}
			ink.A = uint8(int(foreground.A) * alpha / 255)
			rect := image.Rect(x+column, top+row, x+column+1, top+row+1).Intersect(bounds)
			if rect.Empty() {
				continue
			}
			draw.Draw(dst, rect, &image.Uniform{C: ink}, image.Point{}, draw.Over)
		}
	}
}

// Font is a loaded provider chain. A Font without providers still answers every code point with the
// missing glyph, so a resource context without fonts degrades to visible boxes instead of failing.
type Font struct {
	providers []provider
	glyphs    map[rune]*Glyph
	missing   map[rune]bool
}

// Load reads the default font of a resource context. Roots follow the pack order of the renderer:
// a later root wins over an earlier one.
//
// The definition itself is merged across the stack, like the client merges it: every root that
// ships assets/minecraft/font/default.json contributes its providers, the highest priority root
// first and the base tree last. A pack that replaces the definition with a provider list of its own
// therefore adds glyphs instead of having to copy the vanilla includes.
func Load(roots []string) *Font {
	font := &Font{glyphs: map[rune]*Glyph{}, missing: map[rune]bool{}}
	copies := readFiles(roots, definitionPath)
	if len(copies) == 0 {
		report("missing %s, every glyph falls back to the missing glyph", definitionPath)
		return font
	}
	for _, data := range copies {
		font.providers = append(font.providers, readProviders(roots, definitionPath, data, 0)...)
	}
	return font
}

// Glyph answers the glyph of one code point, asking the providers in declaration order.
func (f *Font) Glyph(r rune) *Glyph {
	if glyph, ok := f.glyphs[r]; ok {
		return glyph
	}
	for _, provider := range f.providers {
		if glyph := provider.glyph(r); glyph != nil {
			f.glyphs[r] = glyph
			return glyph
		}
	}
	if !f.missing[r] {
		f.missing[r] = true
		report("no font provider covers U+%04X, drawing the missing glyph", r)
	}
	glyph := missingGlyph()
	f.glyphs[r] = glyph
	return glyph
}

// Advance is the advance of one code point in font pixels.
func (f *Font) Advance(r rune) int {
	return f.Glyph(r).Advance
}

// Width is the advance of plain text, summed per code point.
func (f *Font) Width(text string) int {
	width := 0
	for _, r := range text {
		width += f.Advance(r)
	}
	return width
}

func missingGlyph() *Glyph {
	coverage := make([]uint8, missingWidth*missingHeight)
	for row := 0; row < missingHeight; row++ {
		for column := 0; column < missingWidth; column++ {
			if row == 0 || row == missingHeight-1 || column == 0 || column == missingWidth-1 {
				coverage[row*missingWidth+column] = 4
			}
		}
	}
	return &Glyph{Advance: missingAdvance, Ascent: Ascent, Width: missingWidth, Height: missingHeight, coverage: coverage}
}

// ---------------------------------------------------------------------------------------------
// Provider loading. The JSON shapes are the client's: a reference inlines another definition, a
// space provider only declares advances, a bitmap provider slices a sheet by the code points of its
// chars grid and a unihex provider reads a zipped Unifont .hex file.
// ---------------------------------------------------------------------------------------------

type provider interface {
	glyph(r rune) *Glyph
}

type providerDefinition struct {
	Type          string            `json:"type"`
	ID            string            `json:"id"`
	File          string            `json:"file"`
	Height        *int              `json:"height"`
	Ascent        *int              `json:"ascent"`
	Chars         []string          `json:"chars"`
	Advances      map[string]any    `json:"advances"`
	HexFile       string            `json:"hex_file"`
	SizeOverrides []sizeOverride    `json:"size_overrides"`
	Filter        map[string]bool   `json:"filter"`
	Providers     []json.RawMessage `json:"providers"`
}

type sizeOverride struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Left  int    `json:"left"`
	Right int    `json:"right"`
}

type fontDefinition struct {
	Providers []providerDefinition `json:"providers"`
}

func readProviders(roots []string, definition string, data []byte, depth int) []provider {
	if depth > maxReferenceDepth {
		report("font definition %s nests too deep, skipping it", definition)
		return nil
	}
	var parsed fontDefinition
	if err := json.Unmarshal(data, &parsed); err != nil {
		report("invalid font definition %s: %v", definition, err)
		return nil
	}
	providers := make([]provider, 0, len(parsed.Providers))
	for _, entry := range parsed.Providers {
		if !selected(entry.Filter) {
			continue
		}
		switch entry.Type {
		case "reference":
			nested := referencePath(entry.ID)
			if nested == definition {
				report("font definition %s references itself, skipping it", definition)
				continue
			}
			nestedCopies := readFiles(roots, nested)
			if len(nestedCopies) == 0 {
				report("missing font definition %s, skipping it", nested)
				continue
			}
			// A reference walks the stack like the definition it comes from, so a pack can include
			// its own definition and still inherit the one underneath.
			for _, nestedData := range nestedCopies {
				providers = append(providers, readProviders(roots, nested, nestedData, depth+1)...)
			}
		case "space":
			providers = append(providers, spaceProvider(entry))
		case "bitmap":
			if bitmap := bitmapProvider(roots, entry); bitmap != nil {
				providers = append(providers, bitmap)
			}
		case "unihex":
			if unihex := unihexProvider(roots, entry); unihex != nil {
				providers = append(providers, unihex)
			}
		default:
			report("unsupported font provider type %q in %s", entry.Type, definition)
		}
	}
	return providers
}

// selected applies the client filter of a provider: every key must match whether that font option
// is enabled. The default font renders with no option enabled, which keeps the bitmap sheets of
// include/default (uniform: false) and drops the Japanese Unifont variant (jp: true), exactly like
// the game does for a Chinese client.
func selected(filter map[string]bool) bool {
	for _, wanted := range filter {
		if wanted {
			return false
		}
	}
	return true
}

func referencePath(id string) string {
	value := id
	if index := strings.Index(value, ":"); index >= 0 {
		value = value[index+1:]
	}
	return "assets/minecraft/font/" + value + ".json"
}

// assetPath resolves a hex_file reference such as minecraft:font/unifont.zip.
func assetPath(id string) string {
	namespace, value := "minecraft", id
	if index := strings.Index(value, ":"); index >= 0 {
		namespace, value = value[:index], value[index+1:]
	}
	return "assets/" + namespace + "/" + value
}

// texturePath resolves a bitmap reference such as minecraft:font/ascii.png.
func texturePath(id string) string {
	namespace, value := "minecraft", id
	if index := strings.Index(value, ":"); index >= 0 {
		namespace, value = value[:index], value[index+1:]
	}
	return "assets/" + namespace + "/textures/" + value
}

type spaceGlyphProvider struct {
	advances map[rune]int
}

func spaceProvider(entry providerDefinition) provider {
	advances := make(map[rune]int, len(entry.Advances))
	for key, value := range entry.Advances {
		runes := []rune(key)
		if len(runes) == 0 {
			continue
		}
		advances[runes[0]] = advanceValue(value)
	}
	return &spaceGlyphProvider{advances: advances}
}

func (p *spaceGlyphProvider) glyph(r rune) *Glyph {
	advance, ok := p.advances[r]
	if !ok {
		return nil
	}
	return &Glyph{Advance: advance}
}

func advanceValue(value any) int {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return int(typed + 0.5)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0
		}
		return int(parsed)
	default:
		return 0
	}
}

// bitmapProvider slices one sheet: each code point of the chars grid selects one cell.
type bitmapProviderData struct {
	sheet      image.Image
	cellWidth  int
	cellHeight int
	// height is the font pixel height the glyphs are drawn at: the declared height, or the client's
	// default of eight. A cell taller than this is resampled into the smaller box.
	height int
	// scale is texels per font pixel, cellHeight/height.
	scale    float64
	ascent   int
	advances map[rune]int
	cells    map[rune][2]int
	glyphs   map[rune]*Glyph
}

func bitmapProvider(roots []string, entry providerDefinition) provider {
	data, ok := readFile(roots, texturePath(entry.File))
	if !ok {
		report("missing font sheet %s, skipping it", entry.File)
		return nil
	}
	sheet, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		report("invalid font sheet %s: %v", entry.File, err)
		return nil
	}
	columns := 1
	for _, row := range entry.Chars {
		columns = max(columns, len([]rune(row)))
	}
	rows := max(1, len(entry.Chars))
	provider := &bitmapProviderData{
		sheet:      sheet,
		cellWidth:  max(1, sheet.Bounds().Dx()/columns),
		cellHeight: max(1, sheet.Bounds().Dy()/rows),
		advances:   map[rune]int{},
		cells:      map[rune][2]int{},
		glyphs:     map[rune]*Glyph{},
	}
	provider.height = defaultGlyphHeight
	if entry.Height != nil && *entry.Height > 0 {
		provider.height = *entry.Height
	}
	provider.scale = float64(provider.cellHeight) / float64(provider.height)
	// The ascent is the distance from the glyph top to the baseline, so it follows the height the
	// client draws the glyph at rather than the texel rows of the sheet.
	provider.ascent = provider.height - 1
	if entry.Ascent != nil {
		provider.ascent = *entry.Ascent
	}
	for key, value := range entry.Advances {
		runes := []rune(key)
		if len(runes) > 0 {
			provider.advances[runes[0]] = advanceValue(value)
		}
	}
	for row, line := range entry.Chars {
		for column, r := range []rune(line) {
			if r != 0 {
				provider.cells[r] = [2]int{column, row}
			}
		}
	}
	return provider
}

func (p *bitmapProviderData) glyph(r rune) *Glyph {
	cell, ok := p.cells[r]
	if !ok {
		return nil
	}
	if glyph, ok := p.glyphs[r]; ok {
		return glyph
	}
	glyph := p.read(r, cell[0]*p.cellWidth, cell[1]*p.cellHeight)
	p.glyphs[r] = glyph
	return glyph
}

// read slices one cell. The client keeps the cell origin and trims only the empty columns on the
// right, so a glyph keeps its left bearing; the advance is that width plus the one pixel gap, unless
// the provider declares an explicit advance. A sheet is painted, not sampled, so every inked texel is
// a fully covered pixel.
//
// A cell whose texel size differs from the declared height becomes one glyph box of font pixels
// instead: height pixels tall and ceil(trimmed/scale) wide, its coverage the share of inked texels
// per box pixel, and the native texel grid travels with it so a device scale of scale or more draws
// the sheet itself. A cell that is already height tall keeps the plain one texel per pixel glyph,
// which is every vanilla sheet.
func (p *bitmapProviderData) read(r rune, originX, originY int) *Glyph {
	trimmed := 0
	for column := p.cellWidth - 1; column >= 0; column-- {
		inked := false
		for row := 0; row < p.cellHeight; row++ {
			if p.pixel(originX+column, originY+row) {
				inked = true
				break
			}
		}
		if inked {
			trimmed = column + 1
			break
		}
	}
	if p.scale == 1 {
		coverage := make([]uint8, trimmed*p.cellHeight)
		ink := make([]uint32, trimmed*p.cellHeight)
		tinted := false
		for row := 0; row < p.cellHeight; row++ {
			for column := 0; column < trimmed; column++ {
				texel := p.texel(originX+column, originY+row)
				if texel == 0 {
					continue
				}
				coverage[row*trimmed+column] = 4
				ink[row*trimmed+column] = texel & 0xFFFFFF
				if texel&0xFFFFFF != 0xFFFFFF {
					tinted = true
				}
			}
		}
		if !tinted {
			// A white sheet is drawn in the text colour, which is every vanilla sheet: the glyph keeps
			// no colour table at all then.
			ink = nil
		}
		return &Glyph{Advance: p.advance(r, trimmed), Ascent: p.ascent, Width: trimmed, Height: p.cellHeight,
			coverage: coverage, ink: ink}
	}
	texels := make([]uint32, trimmed*p.cellHeight)
	for row := 0; row < p.cellHeight; row++ {
		for column := 0; column < trimmed; column++ {
			texels[row*trimmed+column] = p.texel(originX+column, originY+row)
		}
	}
	width := int(math.Ceil(float64(trimmed) / p.scale))
	coverage, ink := resampleTexels(texels, trimmed, p.cellHeight, width, p.height, p.scale)
	return &Glyph{Advance: p.advance(r, width), Ascent: p.ascent, Width: width, Height: p.height,
		coverage: coverage, ink: ink, texels: texels, texelWidth: trimmed, texelHeight: p.cellHeight, texelScale: p.scale}
}

// resampleTexels box filters a texel cell into the coverage of one font pixel per box pixel, in
// quarters: a box pixel answers the share of its own area that inked texels cover, and the part of
// the box that reaches past the trimmed cell counts as empty. At a scale of two a two by two texel
// block therefore lands exactly on one pixel of the halved glyph. The colour of each box pixel is the
// inked share's average, or nil for the whole glyph when the cell is pure white.
func resampleTexels(texels []uint32, texelWidth, texelHeight, width, height int, scale float64) ([]uint8, []uint32) {
	coverage := make([]uint8, width*height)
	ink := make([]uint32, width*height)
	tinted := false
	area := scale * scale
	for row := 0; row < height; row++ {
		rowLow := float64(row) * scale
		rowHigh := rowLow + scale
		for column := 0; column < width; column++ {
			columnLow := float64(column) * scale
			columnHigh := columnLow + scale
			lit := 0.0
			var red, green, blue float64
			for texelRow := max(0, int(math.Floor(rowLow))); texelRow < min(texelHeight, int(math.Ceil(rowHigh))); texelRow++ {
				rowWeight := math.Min(rowHigh, float64(texelRow+1)) - math.Max(rowLow, float64(texelRow))
				if rowWeight <= 0 {
					continue
				}
				base := texelRow * texelWidth
				for texelColumn := max(0, int(math.Floor(columnLow))); texelColumn < min(texelWidth, int(math.Ceil(columnHigh))); texelColumn++ {
					columnWeight := math.Min(columnHigh, float64(texelColumn+1)) - math.Max(columnLow, float64(texelColumn))
					if columnWeight <= 0 {
						continue
					}
					texel := texels[base+texelColumn]
					weight := columnWeight * rowWeight * float64(texel>>24) / 255
					if weight <= 0 {
						continue
					}
					lit += weight
					red += weight * float64(texel>>16&0xFF)
					green += weight * float64(texel>>8&0xFF)
					blue += weight * float64(texel&0xFF)
				}
			}
			if lit <= 0 {
				continue
			}
			coverage[row*width+column] = uint8(math.Min(4, math.Round(lit/area*4)))
			colour := uint32(math.Round(red/lit))<<16 | uint32(math.Round(green/lit))<<8 | uint32(math.Round(blue/lit))
			ink[row*width+column] = colour
			if colour != 0xFFFFFF {
				tinted = true
			}
		}
	}
	if !tinted {
		ink = nil
	}
	return coverage, ink
}

func (p *bitmapProviderData) advance(r rune, width int) int {
	if advance, ok := p.advances[r]; ok {
		return advance
	}
	return width + 1
}

func (p *bitmapProviderData) pixel(x, y int) bool {
	return p.texel(x, y)>>24 != 0
}

// texel answers one texel of the sheet as straight (un premultiplied) 0xAARRGGBB, which is the form
// a glyph keeps so that a sheet with colours of its own draws them.
func (p *bitmapProviderData) texel(x, y int) uint32 {
	bounds := p.sheet.Bounds()
	if x < 0 || y < 0 || x >= bounds.Dx() || y >= bounds.Dy() {
		return 0
	}
	value := color.NRGBAModel.Convert(p.sheet.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
	if value.A == 0 {
		return 0
	}
	return uint32(value.A)<<24 | uint32(value.R)<<16 | uint32(value.G)<<8 | uint32(value.B)
}

// unihexProvider renders the Unifont bitmap of a code point. The bitmap is sixteen texels tall and
// the client draws it at half scale, so one glyph covers eight pixels of the eight pixel text cell
// with its top edge on the ascent line. Every output pixel therefore sits on a two by two block of
// texels, and the share of lit texels in that block becomes its coverage: a one texel stroke stays
// visible as a faint pixel instead of being voted away, and the glyph keeps the half pixel edge the
// client has at GUI scale one.
type unihexProviderData struct {
	bitmaps   map[rune][]int32
	overrides []sizeOverride
	glyphs    map[rune]*Glyph
}

func unihexProvider(roots []string, entry providerDefinition) provider {
	data, ok := readFile(roots, assetPath(entry.HexFile))
	if !ok {
		report("missing unihex file %s, skipping it", entry.HexFile)
		return nil
	}
	bitmaps := map[rune][]int32{}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		report("invalid unihex file %s: %v", entry.HexFile, err)
		return nil
	}
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, ".hex") {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			report("invalid unihex entry %s: %v", file.Name, err)
			continue
		}
		readUnifont(stream, bitmaps)
		_ = stream.Close()
	}
	return &unihexProviderData{bitmaps: bitmaps, overrides: entry.SizeOverrides, glyphs: map[rune]*Glyph{}}
}

// readUnifont reads one Unifont hex stream into left aligned 32 bit rows. Rows may be 8, 16, 24 or
// 32 pixels wide and are shifted so the leftmost pixel sits in bit 31, which makes the size
// override columns absolute.
func readUnifont(input io.Reader, bitmaps map[rune][]int32) {
	data, err := io.ReadAll(input)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		separator := strings.Index(line, ":")
		if separator <= 0 {
			continue
		}
		var codePoint int64
		if _, err := fmt.Sscanf(strings.TrimSpace(line[:separator]), "%x", &codePoint); err != nil {
			continue
		}
		bits := strings.TrimSpace(line[separator+1:])
		pixels := len(bits) / 4
		if pixels != 8 && pixels != 16 && pixels != 24 && pixels != 32 || len(bits) != pixels*4 {
			continue
		}
		rows := make([]int32, unifhexRows)
		shift := uint(32 - pixels)
		perRow := pixels / 4
		for row := 0; row < unifhexRows; row++ {
			var value int64
			for digit := 0; digit < perRow; digit++ {
				index := row*perRow + digit
				if index >= len(bits) {
					break
				}
				value = value<<4 | int64(hexDigit(bits[index]))
			}
			rows[row] = int32(value) << shift
		}
		bitmaps[rune(codePoint)] = rows
	}
}

const unifhexRows = 16

func hexDigit(value byte) int {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0')
	case value >= 'a' && value <= 'f':
		return int(value-'a') + 10
	case value >= 'A' && value <= 'F':
		return int(value-'A') + 10
	default:
		return 0
	}
}

func (p *unihexProviderData) glyph(r rune) *Glyph {
	if glyph, ok := p.glyphs[r]; ok {
		return glyph
	}
	glyph := p.read(r)
	p.glyphs[r] = glyph
	return glyph
}

func (p *unihexProviderData) read(r rune) *Glyph {
	bitmap, ok := p.bitmaps[r]
	if !ok {
		return nil
	}
	ink := int32(0)
	for _, row := range bitmap {
		ink |= row
	}
	left, right := unihexDefaultLeft, unihexDefaultRight
	if override, found := p.overrideOf(r); found {
		left, right = override.Left, override.Right
	} else if ink != 0 {
		left = leadingZeros(ink)
		right = 32 - trailingZeros(ink) - 1
	}
	texels := right - left + 1
	width := (texels + 1) / 2
	coverage := make([]uint8, width*unihexCell)
	for row := 0; row < unihexCell; row++ {
		for column := 0; column < width; column++ {
			lit, seen := 0, 0
			for dy := 0; dy < 2; dy++ {
				sourceY := row*2 + dy
				if sourceY >= unifhexRows {
					continue
				}
				for dx := 0; dx < 2; dx++ {
					sourceX := left + column*2 + dx
					if sourceX > right {
						continue
					}
					seen++
					if bitmap[sourceY]>>uint(31-sourceX)&1 != 0 {
						lit++
					}
				}
			}
			if seen > 0 {
				// Round the share of lit texels to the nearest quarter, so a fully inked block is
				// opaque and a single lit texel stays faintly visible.
				coverage[row*width+column] = uint8((lit*4 + seen/2) / seen)
			}
		}
	}
	// The native grid travels with the glyph: one texel per cell of the same window as the coverage,
	// so drawing at twice the size can put one texel on one device pixel.
	grid := make([]uint32, unifhexRows*texels)
	for row := 0; row < unifhexRows; row++ {
		for column := 0; column < texels; column++ {
			if bitmap[row]>>uint(31-(left+column))&1 != 0 {
				grid[row*texels+column] = 0xFFFFFFFF
			}
		}
	}
	return &Glyph{Advance: texels/2 + 1, Ascent: Ascent, Width: width, Height: unihexCell, coverage: coverage,
		texels: grid, texelWidth: texels, texelHeight: unifhexRows, texelScale: 2}
}

func (p *unihexProviderData) overrideOf(r rune) (sizeOverride, bool) {
	for _, override := range p.overrides {
		from, to := []rune(override.From), []rune(override.To)
		if len(from) == 0 || len(to) == 0 {
			continue
		}
		if r >= from[0] && r <= to[0] {
			return override, true
		}
	}
	return sizeOverride{}, false
}

func leadingZeros(value int32) int {
	count := 0
	for bit := 31; bit >= 0; bit-- {
		if value>>uint(bit)&1 != 0 {
			return count
		}
		count++
	}
	return 32
}

func trailingZeros(value int32) int {
	count := 0
	for bit := 0; bit < 32; bit++ {
		if value>>uint(bit)&1 != 0 {
			return count
		}
		count++
	}
	return 32
}

// ---------------------------------------------------------------------------------------------
// Resource access
// ---------------------------------------------------------------------------------------------

// readFile reads a resource relative path from the pack roots, the last root winning the way the
// renderer's own stack resolves overlaps.
func readFile(roots []string, relative string) ([]byte, bool) {
	copies := readFiles(roots, relative)
	if len(copies) == 0 {
		return nil, false
	}
	return copies[0], true
}

// readFiles reads every copy of one relative path, the highest priority root first. A few resource
// formats are cumulative on the client — the provider list of a font definition is the one this
// package reads that way — so they have to see the whole stack instead of only the winning file.
func readFiles(roots []string, relative string) [][]byte {
	if relative == "" || strings.HasPrefix(relative, "/") || strings.Contains(relative, "..") {
		return nil
	}
	clean := path.Clean(relative)
	var files [][]byte
	for index := len(roots) - 1; index >= 0; index-- {
		if roots[index] == "" {
			continue
		}
		candidate := filepath.Join(roots[index], filepath.FromSlash(clean))
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		files = append(files, data)
	}
	return files
}

// reported deduplicates the diagnostics so a broken pack is mentioned once per reason.
var reported = map[string]bool{}

func report(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if reported[message] {
		return
	}
	reported[message] = true
	fmt.Fprintln(os.Stderr, message)
}
