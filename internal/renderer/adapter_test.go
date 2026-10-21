package renderer

import (
	"bytes"
	"context"
	"image"
	"log/slog"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

func TestPrimitiveRenderMapScalesEverySourcePixel(t *testing.T) {
	pixels := make([]int, 128*128)
	for index := range pixels {
		pixels[index] = 1 + index%255
	}
	result, err := (PrimitiveAdapter{}).RenderMap(context.Background(), resources.Context{}, model.MapData{Pixels: pixels}, 256, 256)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds() != image.Rect(0, 0, 256, 256) {
		t.Fatalf("bounds = %v", result.Bounds())
	}
	for _, point := range []image.Point{{0, 0}, {255, 0}, {0, 255}, {255, 255}, {128, 128}} {
		r, g, b, a := result.At(point.X, point.Y).RGBA()
		if a == 0 || (r == 0 && g == 0 && b == 0) {
			t.Fatalf("pixel %v is unexpectedly empty: rgba=%d,%d,%d,%d", point, r, g, b, a)
		}
	}
}

// The map renderer frames the 128 pixel grid with the parchment border, so the default output
// size is 140 and keeps every map pixel at a one to one scale.
func TestCompositorMapDefaultsToPixelExactMap(t *testing.T) {
	compositor := NewCompositor(PrimitiveAdapter{})
	pixels := make([]int, 128*128)
	for index := range pixels {
		pixels[index] = 1 + index%255
	}
	result, err := compositor.Render(context.Background(), model.Request{Type: model.TypeMap}, &model.MapData{Pixels: pixels}, resources.Context{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds() != image.Rect(0, 0, 140, 140) {
		t.Fatalf("bounds = %v, want 140x140", result.Bounds())
	}
}

// A renderer that produced a picture can still have something to say — a resource pack without a
// glyph is the common case — and that copy is the only explanation of why a label looks empty. It
// has to reach the log, but a run that misses Thai, Chinese and Hangul would otherwise repeat one
// line per code point.
func TestLogRendererDiagnosticsSummarizesLongOutput(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logRendererDiagnostics("\n")
	if output.Len() != 0 {
		t.Fatalf("an empty diagnosis must stay quiet: %s", output.String())
	}

	logRendererDiagnostics("no font provider covers U+4E2D, drawing the missing glyph\nextra line\nthird\nfourth\nfifth\n")
	message := output.String()
	if !strings.Contains(message, "renderer diagnostics") {
		t.Fatalf("the diagnosis was not logged: %s", message)
	}
	if !strings.Contains(message, "U+4E2D") {
		t.Fatalf("the first diagnosis line is missing: %s", message)
	}
	if !strings.Contains(message, "(+1 more)") {
		t.Fatalf("the summary does not count the dropped lines: %s", message)
	}
	if strings.Contains(message, "fifth") {
		t.Fatalf("the summary copied every line: %s", message)
	}
}
