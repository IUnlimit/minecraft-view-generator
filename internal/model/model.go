package model

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	SupportedVersion = "1.21.11"
	OutputPNG        = "png"
	OutputBase64     = "base64"

	TypeItem            = "item"
	TypeTooltip         = "tooltip"
	TypeInventory       = "inventory"
	TypeItemContainer   = "item_container"
	TypeEnderchest      = "enderchest"
	TypePlayerInventory = "playerInventory"
	TypeSkin            = "skin"
	TypePlayerList      = "playerlist"
	TypeMap             = "map"
	TypeAchievement     = "achievement"
)

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var supportedTypes = map[string]struct{}{
	TypeItem: {}, TypeTooltip: {}, TypeInventory: {}, TypeItemContainer: {},
	TypeEnderchest: {}, TypePlayerInventory: {}, TypeSkin: {}, TypePlayerList: {},
	TypeMap: {}, TypeAchievement: {},
}

// SupportedTypes returns every render type in the order the API documents them.
func SupportedTypes() []string {
	return []string{
		TypeItem, TypeTooltip, TypeInventory, TypeItemContainer, TypeEnderchest,
		TypePlayerInventory, TypeSkin, TypePlayerList, TypeMap, TypeAchievement,
	}
}

type Request struct {
	Version string          `json:"version"`
	Type    string          `json:"type"`
	Output  string          `json:"output"`
	Data    json.RawMessage `json:"data"`
	Options Options         `json:"options,omitempty"`
}

type Options struct {
	Projection  string `json:"projection,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Background  string `json:"background,omitempty"`
	Scale       int    `json:"scale,omitempty"`
	Frame       int    `json:"frame,omitempty"`
	Transparent bool   `json:"transparent,omitempty"`
}

type Item struct {
	ID              string            `json:"id"`
	Count           int               `json:"count,omitempty"`
	Damage          int               `json:"damage,omitempty"`
	MaxDamage       int               `json:"maxDamage,omitempty"`
	CustomModelData *int              `json:"customModelData,omitempty"`
	Components      json.RawMessage   `json:"components,omitempty"`
	NBT             json.RawMessage   `json:"nbt,omitempty"`
	Name            string            `json:"name,omitempty"`
	Lore            []string          `json:"lore,omitempty"`
	Enchantments    []string          `json:"enchantments,omitempty"`
	Texture         string            `json:"texture,omitempty"`
	Properties      map[string]string `json:"properties,omitempty"`
}

type Slot struct {
	Slot  int  `json:"slot"`
	Item  Item `json:"item"`
	Empty bool `json:"empty,omitempty"`
}

type ContainerData struct {
	Title      string `json:"title,omitempty"`
	Container  string `json:"container,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Columns    int    `json:"columns,omitempty"`
	Slots      []Slot `json:"slots,omitempty"`
	Items      []Slot `json:"items,omitempty"`
	Selected   int    `json:"selected,omitempty"`
	Hotbar     bool   `json:"hotbar,omitempty"`
	Background string `json:"background,omitempty"`
}

type TooltipData struct {
	Item       Item     `json:"item"`
	Title      string   `json:"title,omitempty"`
	Lines      []string `json:"lines,omitempty"`
	Advanced   bool     `json:"advanced,omitempty"`
	Icon       bool     `json:"icon,omitempty"`
	Background string   `json:"background,omitempty"`
}

type SkinData struct {
	Texture    string   `json:"texture,omitempty"`
	TextureURL string   `json:"textureUrl,omitempty"`
	Username   string   `json:"username,omitempty"`
	Model      string   `json:"model,omitempty"`
	Slim       bool     `json:"slim,omitempty"`
	Cape       string   `json:"cape,omitempty"`
	OuterLayer bool     `json:"outerLayer"`
	Properties []string `json:"properties,omitempty"`
}

type PlayerInventoryData struct {
	Title     string   `json:"title,omitempty"`
	Skin      SkinData `json:"skin"`
	Slots     []Slot   `json:"slots,omitempty"`
	Armor     []Slot   `json:"armor,omitempty"`
	Offhand   *Item    `json:"offhand,omitempty"`
	MainHand  *Item    `json:"mainHand,omitempty"`
	Container string   `json:"container,omitempty"`
}

type PlayerListEntry struct {
	Name     string `json:"name"`
	Display  string `json:"display,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Texture  string `json:"texture,omitempty"`
	Ping     int    `json:"ping,omitempty"`
	GameMode string `json:"gameMode,omitempty"`
}

type PlayerListData struct {
	Header   string            `json:"header,omitempty"`
	Footer   string            `json:"footer,omitempty"`
	Players  []PlayerListEntry `json:"players,omitempty"`
	Columns  int               `json:"columns,omitempty"`
	MaxWidth int               `json:"maxWidth,omitempty"`
}

type MapMarker struct {
	X        int    `json:"x"`
	Z        int    `json:"z"`
	Rotation int    `json:"rotation,omitempty"`
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
}

type MapData struct {
	Pixels    []int       `json:"pixels,omitempty"`
	PixelData string      `json:"pixelData,omitempty"`
	Markers   []MapMarker `json:"markers,omitempty"`
	Scale     int         `json:"scale,omitempty"`
	Locked    bool        `json:"locked,omitempty"`
	Dimension string      `json:"dimension,omitempty"`
}

type AchievementData struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Icon        Item   `json:"icon"`
	Completed   bool   `json:"completed,omitempty"`
	Frame       string `json:"frame,omitempty"`
	Parent      string `json:"parent,omitempty"`
}

func (r Request) Validate() error {
	r.Version = strings.TrimSpace(r.Version)
	if !versionPattern.MatchString(r.Version) {
		return fmt.Errorf("version must contain 1-64 letters, digits, dots, underscores, or hyphens")
	}
	if r.Version != SupportedVersion {
		return fmt.Errorf("only Minecraft version %q is supported", SupportedVersion)
	}
	if _, ok := supportedTypes[r.Type]; !ok {
		return fmt.Errorf("unsupported render type %q", r.Type)
	}
	if r.Output != OutputPNG && r.Output != OutputBase64 {
		return fmt.Errorf("output must be %q or %q", OutputPNG, OutputBase64)
	}
	if len(bytes.TrimSpace(r.Data)) == 0 || bytes.Equal(bytes.TrimSpace(r.Data), []byte("null")) {
		return fmt.Errorf("data is required")
	}
	if r.Options.Width < 0 || r.Options.Height < 0 || r.Options.Scale < 0 || r.Options.Frame < 0 {
		return fmt.Errorf("image options cannot be negative")
	}
	if r.Options.Scale > 4 {
		return fmt.Errorf("image scale cannot exceed 4")
	}
	if r.Options.Projection != "" && r.Options.Projection != "gui" && r.Options.Projection != "orthographic" {
		return fmt.Errorf("unsupported projection %q", r.Options.Projection)
	}
	return nil
}

func (r Request) DecodeData() (any, error) {
	var value any
	switch r.Type {
	case TypeItem:
		value = &Item{}
	case TypeTooltip:
		value = &TooltipData{}
	case TypeInventory, TypeItemContainer, TypeEnderchest:
		value = &ContainerData{}
	case TypeSkin:
		value = &SkinData{}
	case TypePlayerInventory:
		value = &PlayerInventoryData{}
	case TypePlayerList:
		value = &PlayerListData{}
	case TypeMap:
		value = &MapData{}
	case TypeAchievement:
		value = &AchievementData{}
	default:
		return nil, fmt.Errorf("unsupported render type %q", r.Type)
	}
	if err := json.Unmarshal(r.Data, value); err != nil {
		return nil, fmt.Errorf("decode %s data: %w", r.Type, err)
	}
	if err := validateData(r.Type, value); err != nil {
		return nil, err
	}
	return value, nil
}

func validateData(kind string, value any) error {
	switch kind {
	case TypeItem:
		item := value.(*Item)
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("item.id is required")
		}
		if item.Count < 0 {
			return fmt.Errorf("item.count cannot be negative")
		}
	case TypeTooltip:
		data := value.(*TooltipData)
		if data.Item.ID == "" && data.Title == "" && len(data.Lines) == 0 {
			return fmt.Errorf("tooltip requires item, title, or lines")
		}
		if len(data.Lines) > 128 {
			return fmt.Errorf("tooltip cannot contain more than 128 lines")
		}
		for _, line := range data.Lines {
			if len([]rune(line)) > 1024 {
				return fmt.Errorf("tooltip line exceeds 1024 characters")
			}
		}
	case TypeInventory, TypeItemContainer, TypeEnderchest:
		data := value.(*ContainerData)
		if kind == TypeItemContainer && strings.TrimSpace(data.Container) == "" {
			return fmt.Errorf("item_container.container is required")
		}
		if kind == TypeEnderchest && ((data.Rows != 0 && data.Rows != 3) || (data.Columns != 0 && data.Columns != 9)) {
			return fmt.Errorf("enderchest supports exactly 3 rows and 9 columns")
		}
		if kind == TypeInventory && ((data.Rows != 0 && (data.Rows < 1 || data.Rows > 6)) || (data.Columns != 0 && data.Columns != 9)) {
			return fmt.Errorf("inventory supports 1-6 rows and exactly 9 columns")
		}
		if data.Rows < 0 || data.Columns < 0 {
			return fmt.Errorf("container rows and columns cannot be negative")
		}
		if len(data.Slots) == 0 && len(data.Items) == 0 {
			return nil
		}
		for _, slot := range append(data.Slots, data.Items...) {
			if slot.Slot < 0 || slot.Slot > 255 {
				return fmt.Errorf("slot index %d is out of range", slot.Slot)
			}
			if !slot.Empty && slot.Item.ID == "" {
				return fmt.Errorf("slot %d has no item id", slot.Slot)
			}
		}
	case TypeSkin:
		data := value.(*SkinData)
		if strings.TrimSpace(data.Texture) == "" {
			return fmt.Errorf("skin.texture is required")
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.Texture)), "http://") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.Texture)), "https://") {
			return fmt.Errorf("skin.texture must not be a network URL")
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.Cape)), "http://") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.Cape)), "https://") {
			return fmt.Errorf("skin.cape must not be a network URL")
		}
		if strings.TrimSpace(data.TextureURL) != "" {
			return fmt.Errorf("skin.textureUrl is not accepted; provide a validated texture")
		}
		if data.Model != "" && data.Model != "classic" && data.Model != "slim" {
			return fmt.Errorf("skin.model must be classic or slim")
		}
	case TypePlayerInventory:
		data := value.(*PlayerInventoryData)
		if strings.TrimSpace(data.Skin.Texture) == "" {
			return fmt.Errorf("playerInventory.skin.texture is required")
		}
		if err := validateSlots(data.Slots); err != nil {
			return err
		}
		if err := validateSlots(data.Armor); err != nil {
			return err
		}
	case TypePlayerList:
		data := value.(*PlayerListData)
		if len(data.Players) > 500 {
			return fmt.Errorf("playerlist cannot contain more than 500 players")
		}
		if data.Columns < 0 || data.Columns > 16 {
			return fmt.Errorf("playerlist columns must be between 0 and 16")
		}
		if data.MaxWidth < 0 || data.MaxWidth > 4096 {
			return fmt.Errorf("playerlist maxWidth must be between 0 and 4096")
		}
		for index, player := range data.Players {
			if strings.TrimSpace(player.Name) == "" && strings.TrimSpace(player.Display) == "" {
				return fmt.Errorf("playerlist player %d has no name", index)
			}
			if strings.TrimSpace(player.Texture) == "" {
				return fmt.Errorf("playerlist player %d (%s) has no avatar texture", index, player.Name)
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(player.Texture)), "http://") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(player.Texture)), "https://") {
				return fmt.Errorf("playerlist avatar textures must not be network URLs")
			}
		}
	case TypeMap:
		data := value.(*MapData)
		if len(data.Pixels) > 16_384 {
			return fmt.Errorf("map pixels cannot exceed 16384 values")
		}
		if data.PixelData != "" {
			decoded, err := decodeMapPixelData(data.PixelData)
			if err != nil {
				return err
			}
			if len(decoded) > 16_384 {
				return fmt.Errorf("map pixelData cannot exceed 16384 bytes")
			}
		}
		if data.Scale < 0 || data.Scale > 4 {
			return fmt.Errorf("map.scale must be between 0 and 4")
		}
		for _, pixel := range data.Pixels {
			if pixel < 0 || pixel > 255 {
				return fmt.Errorf("map pixel value %d is out of range", pixel)
			}
		}
		for _, marker := range data.Markers {
			if marker.X < 0 || marker.X >= 128 || marker.Z < 0 || marker.Z >= 128 {
				return fmt.Errorf("map marker coordinates must be between 0 and 127")
			}
			if marker.Rotation < 0 || marker.Rotation > 15 {
				return fmt.Errorf("map marker rotation must be between 0 and 15")
			}
		}
	case TypeAchievement:
		data := value.(*AchievementData)
		if strings.TrimSpace(data.Title) == "" {
			return fmt.Errorf("achievement.title is required")
		}
		if strings.TrimSpace(data.Icon.ID) == "" {
			return fmt.Errorf("achievement.icon.id is required")
		}
		if data.Frame != "" && data.Frame != "task" && data.Frame != "goal" && data.Frame != "challenge" {
			return fmt.Errorf("achievement.frame must be task, goal, or challenge")
		}
		if len([]rune(data.Title)) > 256 || len([]rune(data.Description)) > 1024 {
			return fmt.Errorf("achievement text is too long")
		}
	}
	return nil
}

func decodeMapPixelData(value string) ([]byte, error) {
	if strings.HasPrefix(value, "base64:") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "base64:"))
		if err != nil {
			return nil, fmt.Errorf("map pixelData base64 is invalid: %w", err)
		}
		return decoded, nil
	}
	return []byte(value), nil
}

func validateSlots(slots []Slot) error {
	for _, slot := range slots {
		if slot.Slot < 0 || slot.Slot > 255 {
			return fmt.Errorf("slot index %d is out of range", slot.Slot)
		}
		if !slot.Empty && slot.Item.ID == "" {
			return fmt.Errorf("slot %d has no item id", slot.Slot)
		}
	}
	return nil
}
