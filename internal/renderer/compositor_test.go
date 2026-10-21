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

	native := renderForSize(t, model.TypeItem, item, model.Options{Width: 32})
	scaled := renderForSize(t, model.TypeItem, item, model.Options{Width: 32, Scale: model.Scale{Ratio: 2}})
	// Every pixel of the native canvas becomes a 2x2 block of the magnified one.
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			want := native.At(native.Bounds().Min.X+x, native.Bounds().Min.Y+y)
			for _, offset := range []image.Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				got := scaled.At(x*2+offset.X, y*2+offset.Y)
				wr, wg, wb, wa := want.RGBA()
				gr, gg, gb, ga := got.RGBA()
				if wr != gr || wg != gg || wb != gb || wa != ga {
					t.Fatalf("pixel %d,%d of the magnified canvas does not repeat the native pixel", x*2+offset.X, y*2+offset.Y)
				}
			}
		}
	}
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
