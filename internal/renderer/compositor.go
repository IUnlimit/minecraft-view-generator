package renderer

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

type Compositor struct {
	Adapter ModelAdapter
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

func (c *Compositor) renderItem(ctx context.Context, options model.Options, item *model.Item, resource resources.Context) (image.Image, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		width = 128
	}
	if height == 0 {
		height = width
	}
	size := width
	if height > size {
		size = height
	}
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
	width, height := options.Width, options.Height
	if width == 0 {
		width = 360
	}
	// A zero height lets the renderer derive the vanilla tooltip box from its content.
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
	cell := 18
	width, height := options.Width, options.Height
	if width == 0 {
		width = columns*cell + 14
	}
	if height == 0 {
		height = rows*cell + 17 + 96
	}
	return c.Adapter.RenderContainer(ctx, resource, kind, *data, width, height)
}

func (c *Compositor) renderSkin(ctx context.Context, options model.Options, data *model.SkinData, resource resources.Context) (image.Image, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		width = 256
	}
	if height == 0 {
		height = 384
	}
	return c.Adapter.RenderPlayer(ctx, resource, *data, Equipment{}, width, height)
}

func (c *Compositor) renderPlayerInventory(ctx context.Context, options model.Options, data *model.PlayerInventoryData, resource resources.Context) (image.Image, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		// The vanilla InventoryScreen is 176x166 and is rendered one to one by default.
		width = 176
	}
	if height == 0 {
		height = 166
	}
	return c.Adapter.RenderPlayerInventory(ctx, resource, *data, width, height)
}

func (c *Compositor) renderPlayerList(ctx context.Context, options model.Options, data *model.PlayerListData, resource resources.Context) (image.Image, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		width = max(360, data.Columns*190)
	}
	return c.Adapter.RenderPlayerList(ctx, resource, *data, width, height)
}

func (c *Compositor) renderMap(ctx context.Context, options model.Options, data *model.MapData, resource resources.Context) (image.Image, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		// 140 keeps the 128 pixel map grid at a one to one scale inside the parchment border.
		width = 140
	}
	if height == 0 {
		height = width
	}
	return c.Adapter.RenderMap(ctx, resource, *data, width, height)
}

func (c *Compositor) renderAchievement(ctx context.Context, options model.Options, data *model.AchievementData, resource resources.Context) (image.Image, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		width = 480
	}
	if height == 0 {
		height = 96
	}
	return c.Adapter.RenderAchievement(ctx, resource, *data, width, height)
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
func defaultSize(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
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
