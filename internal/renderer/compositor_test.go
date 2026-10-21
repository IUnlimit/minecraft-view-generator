package renderer

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

func renderForSize(t *testing.T, kind string, data any, options model.Options) image.Image {
	t.Helper()
	compositor := NewCompositor(PrimitiveAdapter{})
	request := model.Request{Version: model.SupportedVersion, Type: kind, Output: model.OutputPNG, Options: options}
	value, err := compositor.Render(context.Background(), request, data, resources.Context{Version: model.SupportedVersion})
	if err != nil {
		t.Fatalf("%s render failed: %v", kind, err)
	}
	return value
}

func requireSize(t *testing.T, value image.Image, width, height int, label string) {
	t.Helper()
	if value.Bounds().Dx() != width || value.Bounds().Dy() != height {
		t.Fatalf("%s is %dx%d, want %dx%d", label, value.Bounds().Dx(), value.Bounds().Dy(), width, height)
	}
}

func TestRenderHonoursExplicitCanvas(t *testing.T) {
	item := &model.Item{ID: "minecraft:diamond"}
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{}), itemCanvasSize, itemCanvasSize, "default item canvas")
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Width: 64}), 64, 64, "width only item canvas")
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Width: 64, Height: 32}), 64, 32, "explicit item canvas")
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Height: 96}), 96, 96, "height only item canvas")
}

// A single explicit dimension keeps the aspect ratio of the natural canvas instead of stretching it.
func TestRenderKeepsNaturalAspectForOneDimension(t *testing.T) {
	skin := &model.SkinData{Texture: "entity/player/wide/steve"}
	requireSize(t, renderForSize(t, model.TypeSkin, skin, model.Options{}), 256, 384, "default skin canvas")
	requireSize(t, renderForSize(t, model.TypeSkin, skin, model.Options{Width: 128}), 128, 192, "width only skin canvas")
	requireSize(t, renderForSize(t, model.TypeSkin, skin, model.Options{Height: 768}), 512, 768, "height only skin canvas")

	achievement := &model.AchievementData{Title: "Stone Age", Icon: model.Item{ID: "minecraft:stone"}}
	requireSize(t, renderForSize(t, model.TypeAchievement, achievement, model.Options{}), 480, 96, "default achievement canvas")
	requireSize(t, renderForSize(t, model.TypeAchievement, achievement, model.Options{Width: 960}), 960, 192, "width only achievement canvas")
}

// A canvas whose height follows the content must leave that axis to the renderer.
func TestRenderLeavesContentDrivenAxisToTheRenderer(t *testing.T) {
	list := &model.PlayerListData{Players: []model.PlayerListEntry{{Name: "Alex"}}}
	requireSize(t, renderForSize(t, model.TypePlayerList, list, model.Options{Width: 400}), 400, 46+26+28, "playerlist width")
	tooltip := &model.TooltipData{Item: model.Item{Lore: []string{"one line"}}}
	if value := renderForSize(t, model.TypeTooltip, tooltip, model.Options{Height: 200}); value.Bounds().Dy() != 200 {
		t.Fatalf("tooltip height = %d, want 200", value.Bounds().Dy())
	}
}

func TestRenderScaleMagnifiesTheCanvas(t *testing.T) {
	item := &model.Item{ID: "minecraft:diamond"}
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Scale: model.Scale{Ratio: 2}}), itemCanvasSize*2, itemCanvasSize*2, "scaled item canvas")
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Width: 32, Scale: model.Scale{Ratio: 3}}), 96, 96, "scaled explicit item canvas")

	// A fractional ratio has no whole number of device pixels per layout pixel, so it keeps the
	// nearest neighbour magnify path: every source pixel becomes one or two destination pixels.
	native := renderForSize(t, model.TypeItem, item, model.Options{Width: 32})
	scaled := renderForSize(t, model.TypeItem, item, model.Options{Width: 32, Scale: model.Scale{Ratio: 1.5}})
	if scaled.Bounds() != image.Rect(0, 0, 48, 48) {
		t.Fatalf("1.5x canvas = %v, want 48x48", scaled.Bounds())
	}
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			want := native.At(min(int(float64(x)/1.5), 31), min(int(float64(y)/1.5), 31))
			got := scaled.At(x, y)
			wr, wg, wb, wa := want.RGBA()
			gr, gg, gb, ga := got.RGBA()
			if wr != gr || wg != gg || wb != gb || wa != ga {
				t.Fatalf("pixel %d,%d of the magnified canvas is not the nearest neighbour sample", x, y)
			}
		}
	}
}

// capturingAdapter records the target of the last render and answers a blank picture of that size,
// which is how a test asserts what the compositor asked a renderer to rasterise.
type capturingAdapter struct {
	last Target
}

func (a *capturingAdapter) capture(target Target) (image.Image, error) {
	a.last = target
	return image.NewRGBA(image.Rect(0, 0, target.PixelWidth(), target.PixelHeight())), nil
}

func (a *capturingAdapter) Backend() string { return "capturing" }

// NativeScale marks the recorder as a renderer that rasterises its own magnification, so the tests
// observe the target a natively scaling backend is asked for.
func (a *capturingAdapter) NativeScale() bool { return true }

// fractionalAdapter is a backend of fixed shape sizes: it draws at the layout resolution and cannot
// take the magnification into its own raster, so the compositor has to magnify what it returns.
type fractionalAdapter struct {
	capturingAdapter
}

func (a *fractionalAdapter) NativeScale() bool { return false }

func (a *capturingAdapter) RenderItem(_ context.Context, _ resources.Context, _ model.Item, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderPlayer(_ context.Context, _ resources.Context, _ model.SkinData, _ Equipment, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderTooltip(_ context.Context, _ resources.Context, _ model.TooltipData, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderContainer(_ context.Context, _ resources.Context, _ string, _ model.ContainerData, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderPlayerInventory(_ context.Context, _ resources.Context, _ model.PlayerInventoryData, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderPlayerList(_ context.Context, _ resources.Context, _ model.PlayerListData, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderMap(_ context.Context, _ resources.Context, _ model.MapData, target Target) (image.Image, error) {
	return a.capture(target)
}

func (a *capturingAdapter) RenderAchievement(_ context.Context, _ resources.Context, _ model.AchievementData, target Target) (image.Image, error) {
	return a.capture(target)
}

// A whole magnification is handed to the renderer instead of being applied to the finished picture,
// so text and 3D models are rasterised at the final resolution. A fractional magnification, a target
// size that is not a whole multiple, and a canvas whose axis follows the content all keep the raster
// magnify path.
func TestIntegerScaleRendersNatively(t *testing.T) {
	item := &model.Item{ID: "minecraft:diamond"}
	skin := &model.SkinData{Texture: "entity/player/wide/steve"}
	tooltip := &model.TooltipData{Item: model.Item{Lore: []string{"one line"}}}
	cases := []struct {
		label      string
		kind       string
		data       any
		options    model.Options
		wantWidth  int
		wantHeight int
		wantScale  int
	}{
		{"whole ratio", model.TypeItem, item, model.Options{Scale: model.Scale{Ratio: 2}}, 128, 128, 2},
		{"triple ratio", model.TypeItem, item, model.Options{Width: 32, Scale: model.Scale{Ratio: 3}}, 32, 32, 3},
		{"fractional ratio", model.TypeItem, item, model.Options{Scale: model.Scale{Ratio: 1.5}}, 128, 128, 1},
		{"whole target size", model.TypeItem, item, model.Options{Scale: model.Scale{Width: 256}}, 128, 128, 2},
		{"odd target size", model.TypeItem, item, model.Options{Scale: model.Scale{Width: 300}}, 128, 128, 1},
		{"whole canvas", model.TypeSkin, skin, model.Options{Height: 768}, 256, 384, 2},
		{"odd canvas", model.TypeSkin, skin, model.Options{Width: 128}, 128, 192, 1},
		{"inventory canvas", model.TypePlayerInventory, &model.PlayerInventoryData{}, model.Options{Width: 704}, 176, 166, 4},
		{"inventory ratio", model.TypePlayerInventory, &model.PlayerInventoryData{}, model.Options{Scale: model.Scale{Ratio: 4}}, 176, 166, 4},
		{"tooltip width only", model.TypeTooltip, tooltip, model.Options{Width: 720}, 720, 0, 1},
		{"tooltip ratio", model.TypeTooltip, tooltip, model.Options{Scale: model.Scale{Ratio: 2}}, 360, 0, 2},
	}
	for _, test := range cases {
		adapter := &capturingAdapter{}
		compositor := NewCompositor(adapter)
		request := model.Request{Version: model.SupportedVersion, Type: test.kind, Output: model.OutputPNG, Options: test.options}
		if _, err := compositor.Render(context.Background(), request, test.data, resources.Context{Version: model.SupportedVersion}); err != nil {
			t.Fatalf("%s: render failed: %v", test.label, err)
		}
		want := Target{Width: test.wantWidth, Height: test.wantHeight, PixelScale: test.wantScale}
		if adapter.last != want {
			t.Fatalf("%s: target = %+v, want %+v", test.label, adapter.last, want)
		}
	}
}

// A backend that cannot rasterise the magnification itself is handed the layout canvas and the
// compositor magnifies its picture, which is how the fallback keeps its text and its icons in their
// vanilla cells instead of growing them inside them.
func TestRenderMagnifiesForABackendThatCannotRasteriseIt(t *testing.T) {
	item := &model.Item{ID: "minecraft:diamond"}
	adapter := &fractionalAdapter{}
	compositor := NewCompositor(adapter)
	request := model.Request{Version: model.SupportedVersion, Type: model.TypeItem, Output: model.OutputPNG, Options: model.Options{Scale: model.Scale{Ratio: 2}}}
	rendered, err := compositor.Render(context.Background(), request, item, resources.Context{Version: model.SupportedVersion})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if want := (Target{Width: itemCanvasSize, Height: itemCanvasSize, PixelScale: 1}); adapter.last != want {
		t.Fatalf("fixed size backend was asked for %+v, want the layout canvas %+v", adapter.last, want)
	}
	requireSize(t, rendered, itemCanvasSize*2, itemCanvasSize*2, "magnified item canvas")
}

// A fixed ratio may be fractional, and the finished size is rounded to the nearest pixel.
func TestRenderScaleAcceptsFractionalRatios(t *testing.T) {
	item := &model.Item{ID: "minecraft:diamond"}
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Scale: model.Scale{Ratio: 1.5}}), 192, 192, "1.5x item canvas")
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Scale: model.Scale{Ratio: 0.5}}), 64, 64, "0.5x item canvas")
}

// A target size is turned into the largest ratio that fits the requested box without stretching.
func TestRenderScaleFitsTheRequestedTargetSize(t *testing.T) {
	item := &model.Item{ID: "minecraft:diamond"}
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Scale: model.Scale{Width: 300}}), 300, 300, "width target item canvas")
	requireSize(t, renderForSize(t, model.TypeItem, item, model.Options{Scale: model.Scale{Width: 300, Height: 200}}), 200, 200, "box target item canvas")

	achievement := &model.AchievementData{Title: "Stone Age", Icon: model.Item{ID: "minecraft:stone"}}
	requireSize(t, renderForSize(t, model.TypeAchievement, achievement, model.Options{Scale: model.Scale{Width: 960, Height: 400}}), 960, 192, "box target achievement canvas")
	requireSize(t, renderForSize(t, model.TypeAchievement, achievement, model.Options{Scale: model.Scale{Height: 192}}), 960, 192, "height target achievement canvas")
}

// The magnified buffer is rejected before it is allocated when it would exceed the output limits.
func TestRenderScaleRejectsAnOversizedTarget(t *testing.T) {
	compositor := NewCompositor(PrimitiveAdapter{})
	compositor.MaxWidth = 400
	compositor.MaxHeight = 400
	request := model.Request{Version: model.SupportedVersion, Type: model.TypeItem, Output: model.OutputPNG, Options: model.Options{Scale: model.Scale{Width: 800}}}
	if _, err := compositor.Render(context.Background(), request, &model.Item{ID: "minecraft:diamond"}, resources.Context{Version: model.SupportedVersion}); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("oversized magnification error = %v, want ErrImageTooLarge", err)
	}
}

// A whole magnification is bounded before the renderer is asked for it, exactly like the magnified
// path, so an impossible output size never allocates a buffer.
func TestIntegerScaleRespectsLimits(t *testing.T) {
	compositor := NewCompositor(&capturingAdapter{})
	compositor.MaxWidth = 400
	compositor.MaxHeight = 400
	request := model.Request{Version: model.SupportedVersion, Type: model.TypePlayerInventory, Output: model.OutputPNG, Options: model.Options{Scale: model.Scale{Ratio: 4}}}
	if _, err := compositor.Render(context.Background(), request, &model.PlayerInventoryData{}, resources.Context{Version: model.SupportedVersion}); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("oversized native render error = %v, want ErrImageTooLarge", err)
	}
}
