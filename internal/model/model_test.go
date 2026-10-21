package model

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestDecodeItem(t *testing.T) {
	request := Request{Version: SupportedVersion, Type: TypeItem, Output: OutputPNG, Data: json.RawMessage(`{"id":"minecraft:diamond","count":2}`)}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	value, err := request.DecodeData()
	if err != nil {
		t.Fatal(err)
	}
	item := value.(*Item)
	if item.ID != "minecraft:diamond" || item.Count != 2 {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestRequestRejectsUnknownType(t *testing.T) {
	request := Request{Version: "1.21.11", Type: "unknown", Output: OutputPNG, Data: json.RawMessage(`{}`)}
	if err := request.Validate(); err == nil {
		t.Fatal("expected unsupported type error")
	}
}

func TestRequestRejectsPathLikeVersion(t *testing.T) {
	request := Request{Version: "../1.21.11", Type: TypeItem, Output: OutputPNG, Data: json.RawMessage(`{"id":"minecraft:stone"}`)}
	if err := request.Validate(); err == nil {
		t.Fatal("expected invalid version error")
	}
}

func TestRequestRejectsOtherMinecraftVersion(t *testing.T) {
	request := Request{Version: "1.21.10", Type: TypeItem, Output: OutputPNG, Data: json.RawMessage(`{"id":"minecraft:stone"}`)}
	if err := request.Validate(); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestItemContainerRequiresContainerKind(t *testing.T) {
	request := Request{Version: SupportedVersion, Type: TypeItemContainer, Output: OutputPNG, Data: json.RawMessage(`{"slots":[]}`)}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := request.DecodeData(); err == nil {
		t.Fatal("expected container kind error")
	}
}

func TestRequestValidatesSkinMapAndAchievementFrames(t *testing.T) {
	skin := Request{Version: SupportedVersion, Type: TypeSkin, Output: OutputPNG, Data: json.RawMessage(`{"texture":"https://example.invalid/skin.png"}`)}
	if _, err := skin.DecodeData(); err == nil {
		t.Fatal("expected texture URL rejection")
	}
	mapRequest := Request{Version: SupportedVersion, Type: TypeMap, Output: OutputPNG, Data: json.RawMessage(`{"pixels":[256]}`)}
	if _, err := mapRequest.DecodeData(); err == nil {
		t.Fatal("expected map palette range error")
	}
	achievement := Request{Version: SupportedVersion, Type: TypeAchievement, Output: OutputPNG, Data: json.RawMessage(`{"title":"Test","icon":{"id":"minecraft:diamond"},"frame":"invalid"}`)}
	if _, err := achievement.DecodeData(); err == nil {
		t.Fatal("expected achievement frame error")
	}
}
func TestSupportedTypesMatchValidator(t *testing.T) {
	types := SupportedTypes()
	if len(types) != len(supportedTypes) {
		t.Fatalf("SupportedTypes returns %d entries, validator knows %d", len(types), len(supportedTypes))
	}
	seen := make(map[string]struct{}, len(types))
	for _, kind := range types {
		if _, ok := supportedTypes[kind]; !ok {
			t.Fatalf("validator does not accept %q", kind)
		}
		if _, duplicate := seen[kind]; duplicate {
			t.Fatalf("duplicate render type %q", kind)
		}
		seen[kind] = struct{}{}
	}
}

func TestMapAcceptsCompletePixelsAndBase64PixelData(t *testing.T) {
	pixels := make([]int, 128*128)
	for index := range pixels {
		pixels[index] = index % 256
	}
	request := Request{Version: SupportedVersion, Type: TypeMap, Output: OutputPNG, Data: mustJSON(t, MapData{Pixels: pixels})}
	if _, err := request.DecodeData(); err != nil {
		t.Fatalf("complete pixels rejected: %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString(make([]byte, 128*128))
	request.Data = mustJSON(t, MapData{PixelData: "base64:" + encoded})
	if _, err := request.DecodeData(); err != nil {
		t.Fatalf("complete base64 pixelData rejected: %v", err)
	}
}

func TestMapRejectsInvalidAndOversizedBase64PixelData(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "invalid", data: "base64:not-valid%%%"},
		{name: "oversized", data: "base64:" + base64.StdEncoding.EncodeToString(make([]byte, 128*128+1))},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := Request{Version: SupportedVersion, Type: TypeMap, Output: OutputPNG, Data: mustJSON(t, MapData{PixelData: test.data})}
			if _, err := request.DecodeData(); err == nil {
				t.Fatal("expected pixelData validation error")
			}
		})
	}
}

func TestPlayerListRequiresAnAvatarTextureForEveryEntry(t *testing.T) {
	cases := []struct {
		name    string
		players []PlayerListEntry
	}{
		{name: "missing", players: []PlayerListEntry{{Name: "Herobrine"}}},
		{name: "network url", players: []PlayerListEntry{{Name: "Steve", Texture: "https://example.com/skin.png"}}},
		{name: "no name", players: []PlayerListEntry{{Texture: "data:image/png;base64,AAAA"}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := Request{Version: SupportedVersion, Type: TypePlayerList, Output: OutputPNG, Data: mustJSON(t, PlayerListData{Players: test.players})}
			if _, err := request.DecodeData(); err == nil {
				t.Fatal("expected playerlist validation error")
			}
		})
	}
	valid := Request{Version: SupportedVersion, Type: TypePlayerList, Output: OutputPNG,
		Data: mustJSON(t, PlayerListData{Players: []PlayerListEntry{{Name: "Steve", Texture: "data:image/png;base64,AAAA"}}})}
	if _, err := valid.DecodeData(); err != nil {
		t.Fatalf("valid playerlist rejected: %v", err)
	}
}

func TestScaleAcceptsNumberRatioAndTargetSize(t *testing.T) {
	cases := []struct {
		name    string
		options string
		ratio   float64
		width   int
		height  int
	}{
		{name: "number", options: `{"scale":2.5}`, ratio: 2.5},
		{name: "zero number", options: `{"scale":0}`},
		{name: "explicit ratio", options: `{"scale":{"ratio":1.5}}`, ratio: 1.5},
		{name: "target width", options: `{"scale":{"width":800}}`, width: 800},
		{name: "target box", options: `{"scale":{"width":800,"height":600}}`, width: 800, height: 600},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var options Options
			if err := json.Unmarshal([]byte(testCase.options), &options); err != nil {
				t.Fatalf("decode scale: %v", err)
			}
			if err := options.Scale.Validate(); err != nil {
				t.Fatalf("validate scale: %v", err)
			}
			if options.Scale.Ratio != testCase.ratio || options.Scale.Width != testCase.width || options.Scale.Height != testCase.height {
				t.Fatalf("scale = %+v, want ratio %g width %d height %d", options.Scale, testCase.ratio, testCase.width, testCase.height)
			}
		})
	}
}

func TestScaleRejectsInvalidForms(t *testing.T) {
	cases := []struct {
		name    string
		options string
	}{
		{name: "unknown key", options: `{"scale":{"ratios":2}}`},
		{name: "empty object", options: `{"scale":{}}`},
		{name: "string", options: `{"scale":"2x"}`},
		{name: "ratio with target", options: `{"scale":{"ratio":2,"width":800}}`},
		{name: "ratio too large", options: `{"scale":17}`},
		{name: "negative ratio", options: `{"scale":-1}`},
		{name: "target too small", options: `{"scale":{"width":8}}`},
		{name: "negative target", options: `{"scale":{"height":-8}}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var options Options
			err := json.Unmarshal([]byte(testCase.options), &options)
			if err == nil {
				err = options.Scale.Validate()
			}
			if err == nil {
				t.Fatalf("%s was accepted", testCase.options)
			}
		})
	}
}

func TestScaleRatioForFitsTheTargetBox(t *testing.T) {
	if ratio := (Scale{Ratio: 2.5}).RatioFor(128, 128); ratio != 2.5 {
		t.Fatalf("fixed ratio = %g, want 2.5", ratio)
	}
	if ratio := (Scale{}).RatioFor(128, 128); ratio != 1 {
		t.Fatalf("zero scale ratio = %g, want 1", ratio)
	}
	if ratio := (Scale{Width: 300}).RatioFor(128, 128); ratio != 300.0/128.0 {
		t.Fatalf("width target ratio = %g", ratio)
	}
	// The height constraint is tighter than the width constraint, so the picture fits the box.
	if ratio := (Scale{Width: 1024, Height: 240}).RatioFor(128, 128); ratio != 240.0/128.0 {
		t.Fatalf("box target ratio = %g, want %g", ratio, 240.0/128.0)
	}
}

func TestTooltipUsesItemLoreAsTheOnlyLineSource(t *testing.T) {
	request := Request{Version: SupportedVersion, Type: TypeTooltip, Output: OutputPNG, Data: json.RawMessage(`{"item":{"id":"minecraft:diamond_sword","lore":["Forged in the deep"]}}`)}
	if _, err := request.DecodeData(); err != nil {
		t.Fatalf("item with lore rejected: %v", err)
	}
	withoutContent := Request{Version: SupportedVersion, Type: TypeTooltip, Output: OutputPNG, Data: json.RawMessage(`{"advanced":true}`)}
	if _, err := withoutContent.DecodeData(); err == nil {
		t.Fatal("tooltip without item or title was accepted")
	}
	longLore := Request{Version: SupportedVersion, Type: TypeTooltip, Output: OutputPNG, Data: json.RawMessage(`{"item":{"id":"minecraft:stone","lore":["` + strings.Repeat("a", 1025) + `"]}}`)}
	if _, err := longLore.DecodeData(); err == nil {
		t.Fatal("oversized lore line was accepted")
	}
	manyLines := make([]string, 129)
	for index := range manyLines {
		manyLines[index] = "line"
	}
	encoded, err := json.Marshal(map[string]any{"item": map[string]any{"id": "minecraft:stone", "lore": manyLines}})
	if err != nil {
		t.Fatal(err)
	}
	tooMany := Request{Version: SupportedVersion, Type: TypeTooltip, Output: OutputPNG, Data: encoded}
	if _, err := tooMany.DecodeData(); err == nil {
		t.Fatal("too many lore lines were accepted")
	}
}

// A custom item is identified by its base material plus the ItemsAdder custom_model_data value, which
// is a separate field from the vanilla customModelData that drives a range_dispatch override.
func TestItemCustomDataDecodesSeparately(t *testing.T) {
	request := Request{Version: SupportedVersion, Type: TypeItem, Output: OutputPNG,
		Data: json.RawMessage(`{"id":"minecraft:paper","customData":1134,"customModelData":7}`)}
	value, err := request.DecodeData()
	if err != nil {
		t.Fatalf("item with customData rejected: %v", err)
	}
	item := value.(*Item)
	if item.CustomData == nil || *item.CustomData != 1134 {
		t.Fatalf("customData = %v, want 1134", item.CustomData)
	}
	if item.CustomModelData == nil || *item.CustomModelData != 7 {
		t.Fatalf("customModelData = %v, want 7", item.CustomModelData)
	}
	negative := Request{Version: SupportedVersion, Type: TypeItem, Output: OutputPNG,
		Data: json.RawMessage(`{"id":"minecraft:paper","customData":-1}`)}
	if _, err := negative.DecodeData(); err == nil {
		t.Fatal("a negative customData was accepted")
	}
}

// The index key is the base material without its namespace and the custom model data value, so a
// request only has to name the vanilla item it is built on.
func TestCustomArtKeyUsesTheBaseMaterial(t *testing.T) {
	if key := CustomArtKey("minecraft:paper", 1134); key != "PAPER#1134" {
		t.Fatalf("key = %q, want PAPER#1134", key)
	}
	if key := CustomArtKey("PAPER", 0); key != "PAPER#0" {
		t.Fatalf("key = %q, want PAPER#0", key)
	}
	if key := CustomArtKey(" fishing_rod ", 39); key != "FISHING_ROD#39" {
		t.Fatalf("key = %q, want FISHING_ROD#39", key)
	}
}

// Every surface that draws an item has to be able to hand its items to the custom art resolver,
// including the slot items stored by value and the optional hand items.
func TestItemsReachEveryItemPosition(t *testing.T) {
	offhand := &Item{ID: "minecraft:shield", CustomData: intPointer(3)}
	cases := map[string]any{
		"item":        &Item{ID: "minecraft:paper", CustomData: intPointer(1)},
		"tooltip":     &TooltipData{Item: Item{ID: "minecraft:paper", CustomData: intPointer(2)}},
		"achievement": &AchievementData{Icon: Item{ID: "minecraft:paper", CustomData: intPointer(4)}},
		"container": &ContainerData{
			Slots: []Slot{{Slot: 0, Item: Item{ID: "minecraft:paper", CustomData: intPointer(5)}}},
			Items: []Slot{{Slot: 1, Item: Item{ID: "minecraft:paper", CustomData: intPointer(6)}}},
		},
		"inventory": &PlayerInventoryData{
			Slots:    []Slot{{Slot: 0, Item: Item{ID: "minecraft:paper", CustomData: intPointer(7)}}},
			Armor:    []Slot{{Slot: 36, Item: Item{ID: "minecraft:paper", CustomData: intPointer(8)}}},
			Offhand:  offhand,
			MainHand: &Item{ID: "minecraft:paper", CustomData: intPointer(9)},
		},
	}
	for name, value := range cases {
		if items := Items(value); len(items) == 0 {
			t.Fatalf("%s exposed no item positions", name)
		}
		for _, item := range Items(value) {
			if item.CustomData == nil {
				t.Fatalf("%s returned an item without its customData", name)
			}
		}
	}
	hand := Items(cases["inventory"])
	if len(hand) != 4 || hand[2] != offhand {
		t.Fatalf("player inventory items = %d, want the four populated positions in slot order", len(hand))
	}
	if value := Items(&SkinData{}); value != nil {
		t.Fatalf("a skin exposed %d items", len(value))
	}
}

func intPointer(value int) *int {
	return &value
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
