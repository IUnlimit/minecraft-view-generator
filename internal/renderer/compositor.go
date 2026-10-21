package renderer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// ErrImageTooLarge reports that the finished picture, magnification included, would exceed the
// configured output limits. The HTTP layer turns it into 422 image_too_large.
var ErrImageTooLarge = errors.New("rendered image exceeds configured limits")

type Compositor struct {
	Adapter ModelAdapter
	// MaxWidth and MaxHeight bound the delivered picture. Zero means unlimited, which is what the
	// unit tests use; the server always sets both from the configuration.
	MaxWidth  int
	MaxHeight int
}

func NewCompositor(adapter ModelAdapter) *Compositor {
	return &Compositor{Adapter: adapter}
}

func (c *Compositor) Render(ctx context.Context, request model.Request, data any, resource resources.Context) (image.Image, error) {
	if c.Adapter == nil {
		return nil, fmt.Errorf("renderer adapter is not configured")
	}
	var (
		rendered image.Image
		err      error
	)
	switch request.Type {
	case model.TypeItem:
		rendered, err = c.renderItem(ctx, request.Options, data.(*model.Item), resource)
	case model.TypeTooltip:
		rendered, err = c.renderTooltip(ctx, request.Options, data.(*model.TooltipData), resource)
	case model.TypeInventory, model.TypeItemContainer, model.TypeEnderchest:
		rendered, err = c.renderContainer(ctx, request.Type, request.Options, data.(*model.ContainerData), resource)
	case model.TypeSkin:
		rendered, err = c.renderSkin(ctx, request.Options, data.(*model.SkinData), resource)
	case model.TypePlayerInventory:
		rendered, err = c.renderPlayerInventory(ctx, request.Options, data.(*model.PlayerInventoryData), resource)
	case model.TypePlayerList:
		rendered, err = c.renderPlayerList(ctx, request.Options, data.(*model.PlayerListData), resource)
	case model.TypeMap:
		rendered, err = c.renderMap(ctx, request.Options, data.(*model.MapData), resource)
	case model.TypeAchievement:
		rendered, err = c.renderAchievement(ctx, request.Options, data.(*model.AchievementData), resource)
	default:
		return nil, fmt.Errorf("unsupported render type %q", request.Type)
	}
	if err != nil {
		return nil, err
	}
	return c.magnify(rendered, request.Options.Scale)
}

// itemCanvasSize is the natural icon size: the item pipeline draws a 0..16 model box into a square
// canvas, so the default icon is square.
const itemCanvasSize = 128

// renderItem draws one item icon. The icon itself is square, so a canvas that is not square pads it
// on the given background.
func (c *Compositor) renderItem(ctx context.Context, options model.Options, item *model.Item, resource resources.Context) (image.Image, error) {
	width, height := resolveCanvasSize(itemCanvasSize, itemCanvasSize, options)
	size := max(width, height)
	icon, err := c.Adapter.RenderItem(ctx, resource, *item, size)
	if err != nil {
		return nil, err
	}
	if width == size && height == size && options.Background == "" && options.Transparent {
		return icon, nil
	}
	canvas := newCanvas(width, height, options)
	drawImage(canvas, icon, 0, 0, width, height)
	return canvas, nil
}

func (c *Compositor) renderTooltip(ctx context.Context, options model.Options, data *model.TooltipData, resource resources.Context) (image.Image, error) {
	// A zero height lets the renderer derive the vanilla tooltip box from its content.
	width, height := resolveCanvasSize(360, 0, options)
	return c.Adapter.RenderTooltip(ctx, resource, *data, width, height)
}

func (c *Compositor) renderContainer(ctx context.Context, kind string, options model.Options, data *model.ContainerData, resource resources.Context) (image.Image, error) {
	columns, rows := data.Columns, data.Rows
	if columns == 0 {
		columns = 9
	}
	if rows == 0 {
		rows = 3
	}
	if columns > 18 || rows > 12 {
		return nil, fmt.Errorf("%s grid exceeds 18x12", kind)
	}
	naturalWidth, naturalHeight := containerCanvasSize(kind, data, rows)
	width, height := resolveCanvasSize(naturalWidth, naturalHeight, options)
	return c.Adapter.RenderContainer(ctx, resource, kind, *data, width, height)
}

// containerCanvasSize returns the native size of the vanilla screen that owns the container
// texture: 176 px wide everywhere, with the generic_54 family growing one 18 px row per container
// row on top of its 17 px frame and 96 px player inventory band.
func containerCanvasSize(kind string, data *model.ContainerData, rows int) (int, int) {
	const (
		width       = 176
		genericTop  = 17
		genericBand = 96
		screenBody  = 166
		hopperBody  = 133
	)
	switch kind {
	case model.TypeItemContainer:
		if strings.EqualFold(strings.TrimSpace(data.Container), "hopper") {
			return width, hopperBody
		}
		return width, screenBody
	case model.TypeEnderchest:
		return width, genericTop + 3*18 + genericBand
	default:
		return width, genericTop + rows*18 + genericBand
	}
}

func (c *Compositor) renderSkin(ctx context.Context, options model.Options, data *model.SkinData, resource resources.Context) (image.Image, error) {
	width, height := resolveCanvasSize(256, 384, options)
	return c.Adapter.RenderPlayer(ctx, resource, *data, Equipment{}, width, height)
}

func (c *Compositor) renderPlayerInventory(ctx context.Context, options model.Options, data *model.PlayerInventoryData, resource resources.Context) (image.Image, error) {
	// The vanilla InventoryScreen is 176x166 and is rendered one to one by default.
	width, height := resolveCanvasSize(176, 166, options)
	return c.Adapter.RenderPlayerInventory(ctx, resource, *data, width, height)
}

func (c *Compositor) renderPlayerList(ctx context.Context, options model.Options, data *model.PlayerListData, resource resources.Context) (image.Image, error) {
	// The height of the tab list follows its rows, so only the width has a natural value.
	width, height := resolveCanvasSize(max(360, data.Columns*190), 0, options)
	return c.Adapter.RenderPlayerList(ctx, resource, *data, width, height)
}

func (c *Compositor) renderMap(ctx context.Context, options model.Options, data *model.MapData, resource resources.Context) (image.Image, error) {
	// 140 keeps the 128 pixel map grid at a one to one scale inside the parchment border.
	width, height := resolveCanvasSize(140, 140, options)
	return c.Adapter.RenderMap(ctx, resource, *data, width, height)
}

func (c *Compositor) renderAchievement(ctx context.Context, options model.Options, data *model.AchievementData, resource resources.Context) (image.Image, error) {
	width, height := resolveCanvasSize(480, 96, options)
	return c.Adapter.RenderAchievement(ctx, resource, *data, width, height)
}

// resolveCanvasSize turns the natural canvas of a render type into the size of the picture. An
// explicit width and height choose the canvas exactly; a single explicit dimension keeps the aspect
// ratio of the natural canvas, and a natural dimension of zero means the renderer derives that axis
// from its content, which is what the tab list and the tooltip do. options.Scale is not part of the
// canvas: Render magnifies the finished picture by it.
func resolveCanvasSize(naturalWidth, naturalHeight int, options model.Options) (int, int) {
	switch {
	case options.Width > 0 && options.Height > 0:
		return options.Width, options.Height
	case options.Width > 0:
		return options.Width, scaleDimension(naturalHeight, options.Width, naturalWidth)
	case options.Height > 0:
		return scaleDimension(naturalWidth, options.Height, naturalHeight), options.Height
	default:
		return naturalWidth, naturalHeight
	}
}

// scaleDimension converts one axis of a natural canvas to the other axis of a requested canvas. It
// answers zero — leave the axis to the renderer — when the natural value is not a fixed number.
func scaleDimension(natural, requested, basis int) int {
	if natural <= 0 || basis <= 0 {
		return 0
	}
	return max(1, (natural*requested+basis/2)/basis)
}

// magnify applies the requested magnification to a finished picture with a nearest neighbour
// filter, which is how a larger vanilla GUI scale magnifies the same frame buffer. The ratio may be
// fractional (the pixels then alternate between one and two output pixels) and may be below one, and
// a target size is turned into the largest ratio that still fits the box. The magnified size is
// checked against the configured limits before the buffer is allocated.
func (c *Compositor) magnify(value image.Image, scale model.Scale) (image.Image, error) {
	bounds := value.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	ratio := scale.RatioFor(width, height)
	if ratio <= 0 || ratio == 1 {
		return value, nil
	}
	grownWidth := max(1, int(math.Round(float64(width)*ratio)))
	grownHeight := max(1, int(math.Round(float64(height)*ratio)))
	if c.MaxWidth > 0 && grownWidth > c.MaxWidth || c.MaxHeight > 0 && grownHeight > c.MaxHeight {
		return nil, fmt.Errorf("%w: %dx%d after the %gx magnification", ErrImageTooLarge, grownWidth, grownHeight, ratio)
	}
	grown := image.NewRGBA(image.Rect(0, 0, grownWidth, grownHeight))
	for y := 0; y < grownHeight; y++ {
		sourceY := bounds.Min.Y + min(int(float64(y)/ratio), height-1)
		for x := 0; x < grownWidth; x++ {
			sourceX := bounds.Min.X + min(int(float64(x)/ratio), width-1)
			grown.Set(x, y, value.At(sourceX, sourceY))
		}
	}
	return grown, nil
}

func newCanvas(width, height int, options model.Options) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	if !options.Transparent {
		draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: parseColor(options.Background)}, image.Point{}, draw.Src)
	}
	return canvas
}

func parseColor(value string) color.RGBA {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) == 6 {
		var r, g, b uint8
		fmt.Sscanf(value, "%02x%02x%02x", &r, &g, &b)
		return color.RGBA{r, g, b, 255}
	}
	return color.RGBA{0, 0, 0, 0}
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
func mapColor(value int) color.RGBA {
	palette := []color.RGBA{{62, 137, 56, 255}, {102, 170, 72, 255}, {189, 175, 98, 255}, {88, 119, 166, 255}, {160, 160, 160, 255}, {210, 210, 210, 255}, {65, 65, 65, 255}, {120, 80, 45, 255}}
	return palette[(value%len(palette)+len(palette))%len(palette)]
}
func drawImage(dst *image.RGBA, src image.Image, x, y, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	scaled := image.NewRGBA(image.Rect(0, 0, width, height))
	srcBounds := src.Bounds()
	for py := 0; py < height; py++ {
		sy := srcBounds.Min.Y + py*srcBounds.Dy()/height
		for px := 0; px < width; px++ {
			sx := srcBounds.Min.X + px*srcBounds.Dx()/width
			scaled.Set(px, py, src.At(sx, sy))
		}
	}
	draw.Draw(dst, image.Rect(x, y, x+width, y+height), scaled, image.Point{}, draw.Over)
}
