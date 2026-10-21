package renderer

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// spaceOnlyRoot is a minimal pack whose definition only declares the vanilla space advance, so
// every other code point draws the missing glyph and the geometry stays predictable.
func spaceOnlyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "assets/minecraft/font/default.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	definition := `{"providers": [{"type": "space", "advances": {" ": 4}}]}`
	if err := os.WriteFile(path, []byte(definition), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// Text is measured and drawn through the Minecraft provider chain, not through a built in glyph
// table: a space costs four pixels and an uncovered code point costs the six pixels of the missing
// glyph box.
func TestTextUsesTheProviderChain(t *testing.T) {
	loaded := fontFor(resources.Context{Root: spaceOnlyRoot(t)})
	if width := textWidth(loaded, "a a", 1); width != 16 {
		t.Fatalf("width = %d, want 16", width)
	}
	if width := textWidth(loaded, "ab", 3); width != 36 {
		t.Fatalf("scaled width = %d, want 36", width)
	}

	canvas := image.NewRGBA(image.Rect(0, 0, 32, 16))
	if drawn := drawText(canvas, loaded, 0, 0, "a a", 1, color.White, 32); drawn != 16 {
		t.Fatalf("drawn width = %d, want 16", drawn)
	}
	if _, _, _, alpha := canvas.At(0, 0).RGBA(); alpha == 0 {
		t.Fatalf("the first glyph was not drawn")
	}
	if _, _, _, alpha := canvas.At(19, 0).RGBA(); alpha != 0 {
		t.Fatalf("the gap after a space is painted")
	}
}

// Text that would cross the available width is left out instead of being clipped mid glyph.
func TestDrawTextStopsBeforeOverflow(t *testing.T) {
	loaded := fontFor(resources.Context{Root: spaceOnlyRoot(t)})
	canvas := image.NewRGBA(image.Rect(0, 0, 64, 16))
	if drawn := drawText(canvas, loaded, 0, 0, "aaaa", 1, color.White, 20); drawn != 18 {
		t.Fatalf("drawn width = %d, want 18", drawn)
	}
	if _, _, _, alpha := canvas.At(20, 0).RGBA(); alpha != 0 {
		t.Fatalf("the glyph that does not fit was drawn")
	}
}

func TestDrawCenteredTextCentresInTheCanvas(t *testing.T) {
	loaded := fontFor(resources.Context{Root: spaceOnlyRoot(t)})
	canvas := image.NewRGBA(image.Rect(0, 0, 40, 16))
	drawCenteredText(canvas, loaded, 0, "aa", 1, color.White)
	if _, _, _, alpha := canvas.At(14, 0).RGBA(); alpha == 0 {
		t.Fatalf("the centred text does not start at column 14")
	}
	if _, _, _, alpha := canvas.At(0, 0).RGBA(); alpha != 0 {
		t.Fatalf("the left margin is painted")
	}
}

// One font is shared per set of roots, so a render does not decode the Unifont archive per text
// call.
func TestFontIsCachedPerRoot(t *testing.T) {
	context := resources.Context{Root: spaceOnlyRoot(t)}
	first, second := fontFor(context), fontFor(context)
	if first != second {
		t.Fatalf("the same context loaded two fonts")
	}
	if first == fontFor(resources.Context{Root: spaceOnlyRoot(t)}) {
		t.Fatalf("distinct roots share one font")
	}
}
