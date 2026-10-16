package model

import (
	"encoding/base64"
	"encoding/json"
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

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
