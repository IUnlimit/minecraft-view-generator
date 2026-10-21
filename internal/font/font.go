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
)

// Glyph is one drawable glyph: its advance, the baseline offset of its top edge and its per pixel
// coverage. Coverage is measured in quarters, from 0 (empty) to 4 (fully inked). The bitmap sheets
// of the vanilla font only ever answer 0 or 4, while a unihex glyph is sixteen texels tall and is
// painted at half scale by the client: a pixel that only covers one or two of its four texels is
// therefore semi transparent, which is the soft edge the game has at GUI scale one.
type Glyph struct {
	Advance  int
	Ascent   int
	Width    int
	Height   int
	coverage []uint8
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

// Draw paints the glyph with its top edge at top and its left bearing at x. A pixel that is only
// partly covered is composited with a matching alpha, so a magnified unihex glyph keeps the soft
// edge the client gets from drawing its texels at half scale instead of turning into a hard block.
// Every write goes through draw.Over, so text stays readable on top of a panel.
func (g *Glyph) Draw(dst *image.RGBA, x, top, scale int, foreground color.RGBA) {
	if dst == nil {
		return
	}
	if scale < 1 {
		scale = 1
	}
	bounds := dst.Bounds()
	for row := 0; row < g.Height; row++ {
		for column := 0; column < g.Width; column++ {
			coverage := g.Coverage(column, row)
			if coverage == 0 {
				continue
			}
			ink := foreground
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

// Font is a loaded provider chain. A Font without providers still answers every code point with the
// missing glyph, so a resource context without fonts degrades to visible boxes instead of failing.
type Font struct {
	providers []provider
	glyphs    map[rune]*Glyph
	missing   map[rune]bool
}

// Load reads the default font of a resource context. Roots follow the pack order of the renderer:
// a later root overrides an earlier one, so resource packs win over the vanilla tree.
func Load(roots []string) *Font {
	font := &Font{glyphs: map[rune]*Glyph{}, missing: map[rune]bool{}}
	data, ok := readFile(roots, definitionPath)
	if !ok {
		report("missing %s, every glyph falls back to the missing glyph", definitionPath)
		return font
	}
	font.providers = readProviders(roots, definitionPath, data, 0)
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
			nestedData, ok := readFile(roots, nested)
			if !ok {
				report("missing font definition %s, skipping it", nested)
				continue
			}
			providers = append(providers, readProviders(roots, nested, nestedData, depth+1)...)
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
	ascent     int
	advances   map[rune]int
	cells      map[rune][2]int
	glyphs     map[rune]*Glyph
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
	provider.ascent = provider.cellHeight - 1
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
func (p *bitmapProviderData) read(r rune, originX, originY int) *Glyph {
	width := 0
	for column := p.cellWidth - 1; column >= 0; column-- {
		inked := false
		for row := 0; row < p.cellHeight; row++ {
			if p.pixel(originX+column, originY+row) {
				inked = true
				break
			}
		}
		if inked {
			width = column + 1
			break
		}
	}
	coverage := make([]uint8, width*p.cellHeight)
	for row := 0; row < p.cellHeight; row++ {
		for column := 0; column < width; column++ {
			if p.pixel(originX+column, originY+row) {
				coverage[row*width+column] = 4
			}
		}
	}
	advance, ok := p.advances[r]
	if !ok {
		advance = width + 1
	}
	return &Glyph{Advance: advance, Ascent: p.ascent, Width: width, Height: p.cellHeight, coverage: coverage}
}

func (p *bitmapProviderData) pixel(x, y int) bool {
	bounds := p.sheet.Bounds()
	if x < 0 || y < 0 || x >= bounds.Dx() || y >= bounds.Dy() {
		return false
	}
	_, _, _, alpha := p.sheet.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
	return alpha != 0
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
	return &Glyph{Advance: texels/2 + 1, Ascent: Ascent, Width: width, Height: unihexCell, coverage: coverage}
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
	if relative == "" || strings.HasPrefix(relative, "/") || strings.Contains(relative, "..") {
		return nil, false
	}
	clean := path.Clean(relative)
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
		return data, true
	}
	return nil, false
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
