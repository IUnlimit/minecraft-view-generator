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
	switch request.Type {
	case model.TypeItem:
		return c.renderItem(ctx, request.Options, data.(*model.Item), resource)
	case model.TypeTooltip:
		return c.renderTooltip(ctx, request.Options, data.(*model.TooltipData), resource)
	case model.TypeInventory, model.TypeItemContainer, model.TypeEnderchest:
		return c.renderContainer(ctx, request.Type, request.Options, data.(*model.ContainerData), resource)
	case model.TypeSkin:
		return c.renderSkin(ctx, request.Options, data.(*model.SkinData), resource)
	case model.TypePlayerInventory:
		return c.renderPlayerInventory(ctx, request.Options, data.(*model.PlayerInventoryData), resource)
	case model.TypePlayerList:
		return c.renderPlayerList(ctx, request.Options, data.(*model.PlayerListData), resource)
	case model.TypeMap:
		return c.renderMap(ctx, request.Options, data.(*model.MapData), resource)
	case model.TypeAchievement:
		return c.renderAchievement(ctx, request.Options, data.(*model.AchievementData), resource)
	default:
		return nil, fmt.Errorf("unsupported render type %q", request.Type)
	}
}

// itemCanvasSize is the natural icon size: the item pipeline draws a 0..16 model box into a square
// canvas, so the default icon is square.
const itemCanvasSize = 128

// targetFor resolves the target of one render type: the layout canvas in vanilla screen units, and
// the whole magnification a renderer can rasterise at. A zero axis follows the content, which is what
// the tooltip and the tab list do.
//
// Two multipliers feed the magnification. A canvas that is an exact multiple of the natural one is
// the same layout at a higher resolution, so the vanilla coordinates are kept and only the pixel
// scale grows; and options.scale is folded in on top of it. Both must be whole numbers: only then
// does one layout pixel map onto whole device pixels.
func targetFor(naturalWidth, naturalHeight int, options model.Options) Target {
	width, height := resolveCanvasSize(naturalWidth, naturalHeight, options)
	factor := exactMultiple(naturalWidth, naturalHeight, width, height)
	scale := integerRatio(options.Scale.RatioFor(width, height))
	if factor > 1 {
		return Target{Width: naturalWidth, Height: naturalHeight, PixelScale: factor * scale}
	}
	return Target{Width: width, Height: height, PixelScale: scale}
}

// exactMultiple answers the factor by which a requested canvas multiplies the natural one, or one
// when the two do not share a whole factor. An axis the renderer derives from its content never
// matches, so only canvases of a fixed layout reach the native path this way.
func exactMultiple(naturalWidth, naturalHeight, width, height int) int {
	if naturalWidth <= 0 || naturalHeight <= 0 || width <= 0 || height <= 0 {
		return 1
	}
	if width%naturalWidth != 0 || height%naturalHeight != 0 {
		return 1
	}
	if factor := width / naturalWidth; factor >= 2 && factor == height/naturalHeight {
		return factor
	}
	return 1
}

// integerRatio answers the whole magnification of a request, or one when it is fractional: a
// fractional ratio has no whole number of device pixels per layout pixel, so it keeps the raster
// magnify path, which is the only exact way to reach that size.
func integerRatio(ratio float64) int {
	scale := int(math.Round(ratio))
	if scale < 2 || math.Abs(ratio-float64(scale)) > 1e-9 {
		return 1
	}
	return scale
}

// target resolves the render target of one type.
func (c *Compositor) target(naturalWidth, naturalHeight int, options model.Options) Target {
	return targetFor(naturalWidth, naturalHeight, options)
}

// rasterises reports whether the adapter draws the magnification into its own raster. Only such a
// backend can be asked for the device pixels of a hidden magnification.
func (c *Compositor) rasterises() bool {
	scaler, ok := c.Adapter.(nativeScaler)
	return ok && scaler.NativeScale()
}

// handoff is the target the adapter is asked for: the layout canvas when the backend cannot rasterise
// the magnification itself, which is every backend of fixed shape sizes.
func (c *Compositor) handoff(target Target) Target {
	if !target.Native() || c.rasterises() {
		return target
	}
	return Target{Width: target.Width, Height: target.Height}
}

// finish applies the magnification the renderer could not rasterise itself. A backend that drew the
// final resolution already is returned untouched; otherwise the layout picture is magnified to the
// device pixels the target promised, and a fractional ratio keeps its own magnification because whole
// device pixels per layout pixel do not exist for it.
func (c *Compositor) finish(rendered image.Image, options model.Options, target Target) (image.Image, error) {
	if target.Native() && c.rasterises() {
		return rendered, nil
	}
	bounds := rendered.Bounds()
	// A target scale below two means the compositor has to reach the size itself, and a ratio below
	// one shrinks, so only a whole target magnification raises the factor.
	factor := options.Scale.RatioFor(bounds.Dx(), bounds.Dy())
	if scale := target.Scale(); scale > 1 {
		factor = math.Max(factor, float64(scale))
	}
	return c.magnify(rendered, model.Scale{Ratio: factor})
}

// bound rejects a picture that would exceed the configured output limits before it is allocated.
func (c *Compositor) bound(target Target) error {
	width, height := target.PixelWidth(), target.PixelHeight()
	if c.MaxWidth > 0 && width > c.MaxWidth || c.MaxHeight > 0 && height > c.MaxHeight {
		return fmt.Errorf("%w: %dx%d", ErrImageTooLarge, width, height)
	}
	return nil
}

// renderItem draws one item icon. The icon itself is square, so a canvas that is not square pads it
// on the given background.
func (c *Compositor) renderItem(ctx context.Context, options model.Options, item *model.Item, resource resources.Context) (image.Image, error) {
	target := c.target(itemCanvasSize, itemCanvasSize, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	// The icon is square, so the renderer paints the largest square that fits the canvas it is handed.
	handed := c.handoff(target)
	size := max(handed.Width, handed.Height)
	icon, err := c.Adapter.RenderItem(ctx, resource, *item, Target{Width: size, Height: size, PixelScale: handed.Scale()})
	if err != nil {
		return nil, err
	}
	width, height := handed.PixelWidth(), handed.PixelHeight()
	if handed.Width == size && handed.Height == size && options.Background == "" && options.Transparent {
		return c.finish(icon, options, target)
	}
	canvas := newCanvas(width, height, options)
	drawImage(canvas, icon, 0, 0, width, height)
	return c.finish(canvas, options, target)
}

func (c *Compositor) renderTooltip(ctx context.Context, options model.Options, data *model.TooltipData, resource resources.Context) (image.Image, error) {
	// A zero height lets the renderer derive the vanilla tooltip box from its content.
	target := c.target(360, 0, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderTooltip(ctx, resource, *data, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
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
	target := c.target(naturalWidth, naturalHeight, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderContainer(ctx, resource, kind, *data, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
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
	target := c.target(256, 384, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderPlayer(ctx, resource, *data, Equipment{}, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
}

func (c *Compositor) renderPlayerInventory(ctx context.Context, options model.Options, data *model.PlayerInventoryData, resource resources.Context) (image.Image, error) {
	// The vanilla InventoryScreen is 176x166 and is rendered one to one by default.
	target := c.target(176, 166, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderPlayerInventory(ctx, resource, *data, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
}

func (c *Compositor) renderPlayerList(ctx context.Context, options model.Options, data *model.PlayerListData, resource resources.Context) (image.Image, error) {
	// The height of the tab list follows its rows, so only the width has a natural value.
	target := c.target(max(360, data.Columns*190), 0, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderPlayerList(ctx, resource, *data, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
}

func (c *Compositor) renderMap(ctx context.Context, options model.Options, data *model.MapData, resource resources.Context) (image.Image, error) {
	// 140 keeps the 128 pixel map grid at a one to one scale inside the parchment border.
	target := c.target(140, 140, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderMap(ctx, resource, *data, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
}

func (c *Compositor) renderAchievement(ctx context.Context, options model.Options, data *model.AchievementData, resource resources.Context) (image.Image, error) {
	target := c.target(480, 96, options)
	if err := c.bound(target); err != nil {
		return nil, err
	}
	rendered, err := c.Adapter.RenderAchievement(ctx, resource, *data, c.handoff(target))
	if err != nil {
		return nil, err
	}
	return c.finish(rendered, options, target)
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
