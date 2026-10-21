package renderer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// ModelAdapter is the only boundary between the Go compositor and a 3D
// renderer. A production deployment can provide a headless asset-renderer
// process without changing the HTTP contract.
type ModelAdapter interface {
	Backend() string
	RenderItem(context.Context, resources.Context, model.Item, int) (image.Image, error)
	RenderPlayer(context.Context, resources.Context, model.SkinData, Equipment, int, int) (image.Image, error)
	RenderTooltip(context.Context, resources.Context, model.TooltipData, int, int) (image.Image, error)
	RenderContainer(context.Context, resources.Context, string, model.ContainerData, int, int) (image.Image, error)
	RenderPlayerInventory(context.Context, resources.Context, model.PlayerInventoryData, int, int) (image.Image, error)
	RenderPlayerList(context.Context, resources.Context, model.PlayerListData, int, int) (image.Image, error)
	RenderMap(context.Context, resources.Context, model.MapData, int, int) (image.Image, error)
	RenderAchievement(context.Context, resources.Context, model.AchievementData, int, int) (image.Image, error)
}

type BackendInfo struct {
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Status     string   `json:"status"`
	Real       bool     `json:"real"`
	Fallback   bool     `json:"fallback"`
	Operations []string `json:"operations"`
	Error      string   `json:"error,omitempty"`
}

type backendInfoProvider interface {
	BackendInfo() BackendInfo
}

func Info(adapter ModelAdapter) BackendInfo {
	if provider, ok := adapter.(backendInfoProvider); ok {
		return provider.BackendInfo()
	}
	return BackendInfo{Name: adapter.Backend(), Version: "unknown", Status: "degraded", Operations: []string{"item", "player", "tooltip", "inventory", "item_container", "enderchest", "player_inventory", "playerlist", "map", "achievement"}}
}

type Equipment struct {
	Armor    []model.Slot `json:"armor,omitempty"`
	MainHand *model.Item  `json:"mainHand,omitempty"`
	Offhand  *model.Item  `json:"offhand,omitempty"`
}

// PrimitiveAdapter is a deterministic, dependency-free fallback. It is useful
// for API operation and tests when an external Minecraft model engine is not
// installed; its health status explicitly reports the fallback backend.
type PrimitiveAdapter struct{}

func (PrimitiveAdapter) Backend() string { return "primitive-fallback" }

func (PrimitiveAdapter) BackendInfo() BackendInfo {
	return BackendInfo{Name: "primitive-fallback", Version: "1.21.11", Status: "degraded", Real: false, Fallback: true, Operations: []string{"item", "player", "tooltip", "inventory", "item_container", "enderchest", "player_inventory", "playerlist", "map", "achievement"}}
}

func (PrimitiveAdapter) RenderItem(_ context.Context, _ resources.Context, item model.Item, size int) (image.Image, error) {
	if size < 16 || size > 1024 {
		return nil, fmt.Errorf("item render size must be between 16 and 1024")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	base := itemColor(item.ID)
	shadow := color.RGBA{R: base.R / 2, G: base.G / 2, B: base.B / 2, A: 255}
	light := color.RGBA{R: min8(int(base.R) + 55), G: min8(int(base.G) + 55), B: min8(int(base.B) + 55), A: 255}
	cx, cy := size/2, size/2
	depth := max(3, size/7)
	front := max(4, size/3)
	points := []image.Point{{cx - front, cy - front/2}, {cx, cy - front}, {cx + front, cy - front/2}, {cx + front, cy + front/2}, {cx, cy + front}, {cx - front, cy + front/2}}
	fillPolygon(canvas, points, base)
	fillPolygon(canvas, []image.Point{{points[1].X, points[1].Y}, {points[2].X, points[2].Y}, {points[2].X, points[2].Y + depth}, {points[1].X, points[1].Y + depth}}, light)
	fillPolygon(canvas, []image.Point{{points[0].X, points[0].Y}, {points[1].X, points[1].Y}, {points[1].X, points[1].Y + depth}, {points[0].X, points[0].Y + depth}}, shadow)
	fillPolygon(canvas, []image.Point{{points[2].X, points[2].Y}, {points[3].X, points[3].Y}, {points[4].X, points[4].Y}, {points[5].X, points[5].Y}, {points[0].X, points[0].Y}, {points[1].X, points[1].Y}}, base)
	if strings.Contains(strings.ToLower(item.ID), "sword") || strings.Contains(strings.ToLower(item.ID), "pickaxe") {
		drawLine(canvas, cx, cy-front, cx+front/2, cy+front, color.RGBA{245, 245, 245, 255}, max(1, size/24))
	}
	return canvas, nil
}

func (PrimitiveAdapter) RenderContainer(ctx context.Context, resource resources.Context, kind string, data model.ContainerData, width, height int) (image.Image, error) {
	textFont := fontFor(resource)
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
	if width == 0 {
		width = columns*cell + 14
	}
	if height == 0 {
		height = rows*cell + 17 + 96
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(canvas, 8, 8, width-16, height-16, color.RGBA{194, 194, 194, 255})
	strokeRect(canvas, 8, 8, width-16, height-16, color.RGBA{55, 55, 55, 255})
	if data.Title != "" {
		drawCenteredText(canvas, textFont, 18, data.Title, 2, color.RGBA{30, 30, 30, 255})
	}
	gridY := 40
	if data.Title == "" {
		gridY = 24
	}
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			x := 16 + column*cell
			y := gridY + row*cell
			fillRect(canvas, x+2, y+2, cell-4, cell-4, color.RGBA{115, 115, 115, 255})
			strokeRect(canvas, x+2, y+2, cell-4, cell-4, color.RGBA{75, 75, 75, 255})
		}
	}
	slots := data.Slots
	if len(slots) == 0 {
		slots = data.Items
	}
	for _, slot := range slots {
		if slot.Empty || slot.Item.ID == "" {
			continue
		}
		row, column := slot.Slot/columns, slot.Slot%columns
		if row >= rows {
			continue
		}
		icon, err := (PrimitiveAdapter{}).RenderItem(ctx, resource, slot.Item, max(16, cell-8))
		if err != nil {
			return nil, err
		}
		drawScaledImage(canvas, icon, 20+column*cell, gridY+row*cell+4, cell-8, cell-8)
		if slot.Item.Count > 1 {
			drawText(canvas, textFont, 20+column*cell+cell-22, gridY+row*cell+cell-16, fmt.Sprint(slot.Item.Count), 1, color.White, 20)
		}
	}
	return canvas, nil
}

func (PrimitiveAdapter) RenderPlayerInventory(ctx context.Context, resource resources.Context, data model.PlayerInventoryData, width, height int) (image.Image, error) {
	if width == 0 {
		width = 640
	}
	if height == 0 {
		height = 420
	}
	playerWidth := min(260, width/2-24)
	if playerWidth < 32 {
		return nil, fmt.Errorf("player inventory width is too small")
	}
	player, err := (PrimitiveAdapter{}).RenderPlayer(ctx, resource, data.Skin, Equipment{Armor: data.Armor, MainHand: data.MainHand, Offhand: data.Offhand}, playerWidth, min(height-60, 360))
	if err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	drawScaledImage(canvas, player, 16, 34, playerWidth, height-60)
	panel, err := (PrimitiveAdapter{}).RenderContainer(ctx, resource, model.TypeInventory, model.ContainerData{Title: data.Title, Rows: 4, Columns: 9, Slots: data.Slots}, width-playerWidth-36, height-24)
	if err != nil {
		return nil, err
	}
	drawScaledImage(canvas, panel, playerWidth+20, 12, width-playerWidth-36, height-24)
	return canvas, nil
}

func (PrimitiveAdapter) RenderPlayerList(_ context.Context, resource resources.Context, data model.PlayerListData, width, height int) (image.Image, error) {
	textFont := fontFor(resource)
	if width == 0 {
		width = max(360, data.Columns*190)
	}
	columns := data.Columns
	if columns == 0 {
		columns = 1
		if len(data.Players) > 12 {
			columns = 2
		}
		if len(data.Players) > 30 {
			columns = 3
		}
	}
	if columns > 8 {
		return nil, fmt.Errorf("playerlist columns exceed 8")
	}
	rows := (len(data.Players) + columns - 1) / columns
	if height == 0 {
		height = 46 + rows*26 + 28
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(canvas, 8, 8, width-16, height-16, color.RGBA{10, 15, 24, 235})
	strokeRect(canvas, 8, 8, width-16, height-16, color.RGBA{60, 75, 95, 255})
	if data.Header != "" {
		drawCenteredText(canvas, textFont, 16, data.Header, 2, color.White)
	}
	for index, player := range data.Players {
		column, row := index%columns, index/columns
		cellWidth := (width - 32) / columns
		x, y := 16+column*cellWidth, 44+row*26
		fillRect(canvas, x, y, 18, 18, color.RGBA{100, 100, 100, 255})
		label := player.Display
		if label == "" {
			label = player.Name
		}
		drawText(canvas, textFont, x+24, y+4, label, 1, color.RGBA{230, 230, 230, 255}, cellWidth-48)
		if player.Ping > 0 {
			drawText(canvas, textFont, x+cellWidth-40, y+4, fmt.Sprint(player.Ping), 1, color.RGBA{160, 190, 220, 255}, 36)
		}
	}
	if data.Footer != "" {
		drawText(canvas, textFont, 16, height-22, data.Footer, 1, color.RGBA{180, 180, 180, 255}, width-32)
	}
	return canvas, nil
}

func (PrimitiveAdapter) RenderMap(_ context.Context, _ resources.Context, data model.MapData, width, height int) (image.Image, error) {
	if width == 0 {
		width = 128
	}
	if height == 0 {
		height = 128
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	pixels := data.Pixels
	if len(pixels) == 0 && data.PixelData != "" {
		pixelData := data.PixelData
		if strings.HasPrefix(pixelData, "base64:") {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pixelData, "base64:"))
			if err != nil {
				return nil, fmt.Errorf("map pixelData base64 is invalid: %w", err)
			}
			pixelData = string(decoded)
		}
		for _, value := range []byte(pixelData) {
			pixels = append(pixels, int(value))
		}
	}
	for index, value := range pixels {
		x, y := index%128, index/128
		if y >= 128 {
			break
		}
		x0, y0 := x*width/128, y*height/128
		x1, y1 := (x+1)*width/128, (y+1)*height/128
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if y1 <= y0 {
			y1 = y0 + 1
		}
		alpha := uint8(255)
		if value == 0 {
			alpha = 0
		}
		fillRect(canvas, x0, y0, x1-x0, y1-y0, color.RGBA{uint8(value), uint8(value), uint8(value), alpha})
	}
	for _, marker := range data.Markers {
		x := clamp(marker.X*width/128, 0, width-1)
		y := clamp(marker.Z*height/128, 0, height-1)
		fillRect(canvas, x-2, y-2, 5, 5, color.RGBA{220, 35, 35, 255})
	}
	return canvas, nil
}

func (PrimitiveAdapter) RenderAchievement(ctx context.Context, resource resources.Context, data model.AchievementData, width, height int) (image.Image, error) {
	textFont := fontFor(resource)
	if width == 0 {
		width = 480
	}
	if height == 0 {
		height = 96
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(canvas, 4, 4, width-8, height-8, color.RGBA{30, 30, 30, 245})
	strokeRect(canvas, 4, 4, width-8, height-8, color.RGBA{220, 170, 45, 255})
	icon, err := (PrimitiveAdapter{}).RenderItem(ctx, resource, data.Icon, max(16, min(80, width/5)))
	if err != nil {
		return nil, err
	}
	drawScaledImage(canvas, icon, 16, 8, min(80, width/5), min(80, height-16))
	drawText(canvas, textFont, 112, 24, data.Title, 2, color.RGBA{255, 215, 80, 255}, width-128)
	drawText(canvas, textFont, 112, 54, data.Description, 1, color.RGBA{230, 230, 230, 255}, width-128)
	return canvas, nil
}

func (PrimitiveAdapter) RenderTooltip(ctx context.Context, resource resources.Context, data model.TooltipData, width, height int) (image.Image, error) {
	textFont := fontFor(resource)
	if width == 0 {
		width = 360
	}
	if height == 0 {
		height = 96 + len(data.Item.Lore)*18
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	fillRect(canvas, 3, 3, width-6, height-6, color.RGBA{22, 19, 30, 245})
	strokeRect(canvas, 3, 3, width-6, height-6, color.RGBA{80, 70, 98, 255})
	if data.Item.ID != "" {
		icon, err := (PrimitiveAdapter{}).RenderItem(ctx, resource, data.Item, max(16, min(width/4, 96)))
		if err != nil {
			return nil, err
		}
		drawScaledImage(canvas, icon, 12, 12, min(width/4, 96), min(width/4, 96))
	}
	x := width / 3
	if x < 42 {
		x = 42
	}
	if data.Title != "" {
		drawText(canvas, textFont, x, 18, data.Title, 2, color.RGBA{255, 255, 255, 255}, width-x-12)
	}
	// Lore is the only free form line source of a tooltip: it sits with the vanilla dark purple of
	// the lore component and explicit formatting codes still override the colour.
	lineY := 42
	for _, line := range data.Item.Lore {
		drawText(canvas, textFont, x, lineY, line, 1, color.RGBA{170, 0, 170, 255}, width-x-12)
		lineY += 16
	}
	return canvas, nil
}

func (PrimitiveAdapter) RenderPlayer(_ context.Context, _ resources.Context, skin model.SkinData, equipment Equipment, width, height int) (image.Image, error) {
	if width < 32 || height < 48 || width > 1024 || height > 1536 {
		return nil, fmt.Errorf("player render dimensions are outside limits")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	base := itemColor(skin.Texture + skin.Username)
	if skin.Slim || skin.Model == "slim" {
		base = color.RGBA{R: min8(int(base.R) + 12), G: min8(int(base.G) + 8), B: min8(int(base.B) + 5), A: 255}
	}
	outline := color.RGBA{18, 22, 32, 255}
	cx := width / 2
	head := max(14, width/5)
	bodyW := max(18, width/4)
	bodyH := max(28, height/3)
	legH := max(20, height/4)
	legW := max(8, bodyW/2-2)
	feetY := height - max(8, height/12)
	bodyY := max(10, height/3)
	// Back legs and body create a readable isometric character silhouette.
	fillRect(canvas, cx-bodyW/2, bodyY, bodyW, bodyH, base)
	strokeRect(canvas, cx-bodyW/2, bodyY, bodyW, bodyH, outline)
	fillRect(canvas, cx-head/2, bodyY-head-4, head, head, base)
	strokeRect(canvas, cx-head/2, bodyY-head-4, head, head, outline)
	fillRect(canvas, cx-bodyW/2-legW-2, feetY-legH, legW, legH, base)
	fillRect(canvas, cx+2, feetY-legH, legW, legH, base)
	strokeRect(canvas, cx-bodyW/2-legW-2, feetY-legH, legW, legH, outline)
	strokeRect(canvas, cx+2, feetY-legH, legW, legH, outline)
	armW := max(7, bodyW/3)
	fillRect(canvas, cx-bodyW/2-armW-3, bodyY+3, armW, bodyH-5, base)
	fillRect(canvas, cx+bodyW/2+3, bodyY+3, armW, bodyH-5, base)
	strokeRect(canvas, cx-bodyW/2-armW-3, bodyY+3, armW, bodyH-5, outline)
	strokeRect(canvas, cx+bodyW/2+3, bodyY+3, armW, bodyH-5, outline)
	if skin.OuterLayer {
		strokeRect(canvas, cx-head/2-2, bodyY-head-6, head+4, head+4, color.RGBA{255, 255, 255, 150})
		strokeRect(canvas, cx-bodyW/2-2, bodyY-2, bodyW+4, bodyH+4, color.RGBA{255, 255, 255, 150})
	}
	if len(equipment.Armor) > 0 {
		armorColor := color.RGBA{90, 110, 145, 210}
		strokeRect(canvas, cx-bodyW/2-1, bodyY-1, bodyW+2, bodyH+2, armorColor)
	}
	if equipment.MainHand != nil {
		drawLine(canvas, cx+bodyW/2+armW+2, bodyY+bodyH/2, cx+bodyW/2+armW+sizeFor(width, 24), bodyY+bodyH/2-sizeFor(height, 20), itemColor(equipment.MainHand.ID), max(2, width/40))
	}
	if equipment.Offhand != nil {
		drawLine(canvas, cx-bodyW/2-armW-2, bodyY+bodyH/2, cx-bodyW/2-armW-sizeFor(width, 24), bodyY+bodyH/2-sizeFor(height, 20), itemColor(equipment.Offhand.ID), max(2, width/40))
	}
	return canvas, nil
}

// CommandAdapter speaks a small JSON-over-stdin protocol to an external
// headless renderer. The executable is invoked directly (never through a
// shell), and output is bounded by the caller's context and HTTP limits.
type CommandAdapter struct {
	Command        string
	MaxOutputBytes int64
	Version        string
}

func (a CommandAdapter) Backend() string { return "java-minecraft-1.21.11" }

func (a CommandAdapter) BackendInfo() BackendInfo {
	version := a.Version
	if version == "" {
		version = "1.21.11"
	}
	info := BackendInfo{Name: a.Backend(), Version: version, Status: "ready", Real: true, Fallback: false, Operations: []string{"item", "player", "tooltip", "inventory", "item_container", "enderchest", "player_inventory", "playerlist", "map", "achievement"}}
	if !commandAvailable(a.Command) {
		info.Status = "degraded"
		info.Error = fmt.Sprintf("renderer command %q is not executable", a.Command)
	}
	return info
}

func commandAvailable(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if strings.Contains(command, string(os.PathSeparator)) {
		info, err := os.Stat(command)
		return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(command)
	return err == nil
}

type UnavailableAdapter struct {
	Reason string
}

func (a UnavailableAdapter) Backend() string { return "unavailable" }

func (a UnavailableAdapter) BackendInfo() BackendInfo {
	return BackendInfo{Name: "unavailable", Version: "1.21.11", Status: "degraded", Real: false, Fallback: false, Operations: []string{"item", "player", "tooltip", "inventory", "item_container", "enderchest", "player_inventory", "playerlist", "map", "achievement"}, Error: a.Reason}
}

func (a UnavailableAdapter) RenderItem(context.Context, resources.Context, model.Item, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderPlayer(context.Context, resources.Context, model.SkinData, Equipment, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderTooltip(context.Context, resources.Context, model.TooltipData, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderContainer(context.Context, resources.Context, string, model.ContainerData, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderPlayerInventory(context.Context, resources.Context, model.PlayerInventoryData, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderPlayerList(context.Context, resources.Context, model.PlayerListData, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderMap(context.Context, resources.Context, model.MapData, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a UnavailableAdapter) RenderAchievement(context.Context, resources.Context, model.AchievementData, int, int) (image.Image, error) {
	return nil, fmt.Errorf("real Minecraft 1.21.11 renderer unavailable: %s", a.Reason)
}

func (a CommandAdapter) RenderItem(ctx context.Context, resource resources.Context, item model.Item, size int) (image.Image, error) {
	payload := commandPayload{Operation: "item", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, Item: item, Width: size, Height: size}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderPlayer(ctx context.Context, resource resources.Context, skin model.SkinData, equipment Equipment, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: "player", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, Skin: skin, Equipment: equipment, Width: width, Height: height}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderTooltip(ctx context.Context, resource resources.Context, data model.TooltipData, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: "tooltip", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, Tooltip: data, Width: width, Height: height}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderContainer(ctx context.Context, resource resources.Context, kind string, data model.ContainerData, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: kind, Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, Container: data, Width: width, Height: height}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderPlayerInventory(ctx context.Context, resource resources.Context, data model.PlayerInventoryData, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: "player_inventory", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, PlayerInventory: data, Width: width, Height: height}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderPlayerList(ctx context.Context, resource resources.Context, data model.PlayerListData, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: "playerlist", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, PlayerList: data, Width: width, Height: height}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderMap(ctx context.Context, resource resources.Context, data model.MapData, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: "map", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, Map: data, Width: width, Height: height}
	return a.run(ctx, payload)
}

func (a CommandAdapter) RenderAchievement(ctx context.Context, resource resources.Context, data model.AchievementData, width, height int) (image.Image, error) {
	payload := commandPayload{Operation: "achievement", Version: resource.Version, ResourceRoot: resource.Root, PackRoots: resource.PackRoots, CustomArt: resource.CustomArt, Achievement: data, Width: width, Height: height}
	return a.run(ctx, payload)
}

type commandPayload struct {
	Operation       string                     `json:"operation"`
	Version         string                     `json:"version"`
	ResourceRoot    string                     `json:"resourceRoot"`
	PackRoots       []string                   `json:"packRoots,omitempty"`
	CustomArt       map[string]model.CustomArt `json:"customArt,omitempty"`
	Item            model.Item                 `json:"item,omitempty"`
	Skin            model.SkinData             `json:"skin,omitempty"`
	Equipment       Equipment                  `json:"equipment,omitempty"`
	Tooltip         model.TooltipData          `json:"tooltip,omitempty"`
	Container       model.ContainerData        `json:"container,omitempty"`
	PlayerInventory model.PlayerInventoryData  `json:"playerInventory,omitempty"`
	PlayerList      model.PlayerListData       `json:"playerList,omitempty"`
	Map             model.MapData              `json:"map,omitempty"`
	Achievement     model.AchievementData      `json:"achievement,omitempty"`
	Width           int                        `json:"width"`
	Height          int                        `json:"height"`
}

func (a CommandAdapter) run(ctx context.Context, payload commandPayload) (image.Image, error) {
	if strings.TrimSpace(a.Command) == "" {
		return nil, fmt.Errorf("renderer command is not configured")
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, a.Command)
	command.Stdin = bytes.NewReader(input)
	limit := a.MaxOutputBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	var stdout limitedBuffer
	stdout.limit = limit
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("external renderer failed: %s", message)
	}
	// A renderer that cannot find the font assets still answers with a picture, in which every
	// label is the missing glyph box. Its stderr is the only place that says so, so it is repeated
	// here instead of being dropped with the exit code.
	logRendererDiagnostics(stderr.String())
	decoded, err := decodePNG(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("external renderer returned invalid PNG: %w", err)
	}
	return decoded, nil
}

// rendererDiagnosticsLines bounds how much of the renderer's stderr the service repeats. Every code
// point without a glyph is one line, so an unbounded copy would bury the log of a single request.
const rendererDiagnosticsLines = 4

// logRendererDiagnostics reports the warnings of a renderer run that still produced a picture.
func logRendererDiagnostics(stderr string) {
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		return
	}
	lines := strings.Split(trimmed, "\n")
	kept := lines
	if len(kept) > rendererDiagnosticsLines {
		kept = kept[:rendererDiagnosticsLines]
	}
	message := strings.Join(kept, "; ")
	if remaining := len(lines) - len(kept); remaining > 0 {
		message += fmt.Sprintf(" (+%d more)", remaining)
	}
	slog.Warn("renderer diagnostics", "message", message)
}

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	if int64(b.Len())+int64(len(value)) > b.limit {
		return 0, fmt.Errorf("renderer output exceeds %d bytes", b.limit)
	}
	return b.Buffer.Write(value)
}

func itemColor(id string) color.RGBA {
	if id == "" {
		id = "unknown"
	}
	var hash uint32 = 2166136261
	for _, value := range id {
		hash ^= uint32(value)
		hash *= 16777619
	}
	return color.RGBA{R: uint8(60 + hash%150), G: uint8(60 + (hash>>8)%150), B: uint8(60 + (hash>>16)%150), A: 255}
}

func decodePNG(data []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(data))
}

func fillPolygon(img *image.RGBA, points []image.Point, c color.Color) {
	if len(points) < 3 {
		return
	}
	minY, maxY := points[0].Y, points[0].Y
	for _, point := range points[1:] {
		if point.Y < minY {
			minY = point.Y
		}
		if point.Y > maxY {
			maxY = point.Y
		}
	}
	for y := minY; y <= maxY; y++ {
		intersections := make([]int, 0, len(points))
		for i, a := range points {
			b := points[(i+1)%len(points)]
			if (a.Y <= y && b.Y > y) || (b.Y <= y && a.Y > y) {
				x := a.X + (y-a.Y)*(b.X-a.X)/(b.Y-a.Y)
				intersections = append(intersections, x)
			}
		}
		for i := 0; i+1 < len(intersections); i += 2 {
			if intersections[i] > intersections[i+1] {
				intersections[i], intersections[i+1] = intersections[i+1], intersections[i]
			}
			for x := intersections[i]; x <= intersections[i+1]; x++ {
				if image.Pt(x, y).In(img.Bounds()) {
					img.Set(x, y, c)
				}
			}
		}
	}
}

func fillRect(img *image.RGBA, x, y, width, height int, c color.Color) {
	draw.Draw(img, image.Rect(x, y, x+width, y+height), &image.Uniform{C: c}, image.Point{}, draw.Over)
}
func strokeRect(img *image.RGBA, x, y, width, height int, c color.Color) {
	drawLine(img, x, y, x+width, y, c, 1)
	drawLine(img, x, y, x, y+height, c, 1)
	drawLine(img, x+width, y, x+width, y+height, c, 1)
	drawLine(img, x, y+height, x+width, y+height, c, 1)
}
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.Color, width int) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		fillRect(img, x0-width/2, y0-width/2, width, width, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min8(value int) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func drawScaledImage(dst *image.RGBA, src image.Image, x, y, width, height int) {
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
func sizeFor(value, divisor int) int {
	if divisor <= 0 {
		return value
	}
	return max(1, value/divisor)
}
