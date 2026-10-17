package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/renderer"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.RequireResources = false
	cfg.AllowFallbackRenderer = true
	manager, err := resources.NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(cfg, manager, renderer.NewCompositor(renderer.PrimitiveAdapter{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHealth(t *testing.T) {
	server := testServer(t)
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d", record.Code)
	}
}

func TestDocsUsesScalar(t *testing.T) {
	server := testServer(t)
	server.cfg.APIKey = "secret"
	for _, path := range []string{"/docs", "/scalar"} {
		record := httptest.NewRecorder()
		server.Handler().ServeHTTP(record, httptest.NewRequest(http.MethodGet, path, nil))
		if record.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, record.Code)
		}
		if contentType := record.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("%s content type = %q", path, contentType)
		}
		body := record.Body.String()
		if !strings.Contains(body, "createApiReference") || !strings.Contains(body, "/api/v1/openapi.json") {
			t.Fatalf("%s does not contain Scalar configuration", path)
		}
	}
}

func TestRenderBase64Item(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"base64","data":{"id":"minecraft:diamond"}}`))
	request.Header.Set("Content-Type", "application/json")
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", record.Code, record.Body.String())
	}
	var value struct {
		Data  string `json:"data"`
		Width int    `json:"width"`
	}
	if err := json.Unmarshal(record.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Data == "" || value.Width != 128 {
		t.Fatalf("unexpected response: %+v", value)
	}
}

func TestRenderAcceptsUnknownItemInContainer(t *testing.T) {
	// A slot the resource pack cannot resolve is the renderer's business: it draws the vanilla
	// missing model instead of failing, so the API must not reject the request up front.
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"inventory","output":"base64","data":{"title":"Chest","rows":3,"columns":9,"slots":[{"slot":0,"item":{"id":"minecraft:not_an_item"}}]}}`))
	request.Header.Set("Content-Type", "application/json")
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", record.Code, record.Body.String())
	}
}

func TestRenderRejectsMissingItemID(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"png","data":{}}`))
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", record.Code)
	}
}

func TestRenderRejectsTinyRequestedImage(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"png","options":{"width":8},"data":{"id":"minecraft:stone"}}`))
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", record.Code)
	}
}

func TestAPIKeyProtectsPrivateRoutes(t *testing.T) {
	server := testServer(t)
	server.cfg.APIKey = "secret"
	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/versions", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/versions", nil)
	authorizedRequest.Header.Set("X-API-Key", "secret")
	authorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d", authorized.Code)
	}
}

func TestRenderRejectsTrailingJSON(t *testing.T) {
	server := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"png","data":{"id":"minecraft:stone"}} {}`))
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", record.Code)
	}
}

// documentExample mirrors one OpenAPI Example Object as Scalar consumes it.
type documentExample struct {
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Value       json.RawMessage `json:"value"`
}

func renderExamplesFromDocument(t *testing.T) map[string]documentExample {
	t.Helper()
	encoded, err := json.Marshal(openAPIDocument)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]struct {
			Post struct {
				RequestBody struct {
					Content map[string]struct {
						Examples map[string]documentExample `json:"examples"`
					} `json:"content"`
				} `json:"requestBody"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	return document.Paths["/api/v1/render"].Post.RequestBody.Content["application/json"].Examples
}

// decodeExample replays exactly what the render handler does with a payload.
func decodeExample(t *testing.T, kind string, payload json.RawMessage) model.Request {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request model.Request
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("example %q does not decode: %v", kind, err)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("example %q is invalid: %v", kind, err)
	}
	if _, err := request.DecodeData(); err != nil {
		t.Fatalf("example %q has invalid data: %v", kind, err)
	}
	return request
}

func assertASCIIText(t *testing.T, label string, values ...string) {
	t.Helper()
	for _, value := range values {
		for _, char := range value {
			if char < 32 || char > 126 {
				t.Fatalf("%s is drawn with the ASCII font, but contains %q in %q", label, char, value)
			}
		}
	}
}

// assertStyledText allows the section sign formatting codes the tooltip renderer consumes, so the
// visible characters still have to be ASCII.
func assertStyledText(t *testing.T, label string, values ...string) {
	t.Helper()
	for _, value := range values {
		runes := []rune(value)
		for index := 0; index < len(runes); index++ {
			char := runes[index]
			if char == '§' {
				if index+1 >= len(runes) {
					t.Fatalf("%s ends with a dangling section sign in %q", label, value)
				}
				code := runes[index+1]
				if !strings.ContainsRune("0123456789abcdefklmnorABCDEFKLMNOR", code) {
					t.Fatalf("%s uses an unsupported formatting code %q in %q", label, code, value)
				}
				index++
				continue
			}
			if char < 32 || char > 126 {
				t.Fatalf("%s is drawn with the ASCII font, but contains %q in %q", label, char, value)
			}
		}
	}
}

func assertSkinTexture(t *testing.T, label, texture string) {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(texture, prefix) {
		t.Fatalf("%s texture %q is not an inline PNG data URI", label, texture)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(texture, prefix))
	if err != nil {
		t.Fatalf("%s texture is not valid base64: %v", label, err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("%s texture is not a PNG: %v", label, err)
	}
	if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
		t.Fatalf("%s texture is %dx%d, want 64x64", label, decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestOpenAPIRenderExamplesCoverSupportedTypes(t *testing.T) {
	examples := renderExamplesFromDocument(t)
	types := model.SupportedTypes()
	if len(examples) != len(types) {
		t.Fatalf("example count = %d, want %d", len(examples), len(types))
	}
	for _, kind := range types {
		example, ok := examples[kind]
		if !ok {
			t.Fatalf("missing prefilled example for %q", kind)
		}
		if strings.TrimSpace(example.Summary) == "" || strings.TrimSpace(example.Description) == "" {
			t.Fatalf("example %q needs a summary and description", kind)
		}
		request := decodeExample(t, kind, example.Value)
		if request.Type != kind {
			t.Fatalf("example %q declares type %q", kind, request.Type)
		}
		if request.Version != model.SupportedVersion {
			t.Fatalf("example %q declares version %q", kind, request.Version)
		}
		if request.Output != model.OutputBase64 {
			t.Fatalf("example %q output = %q", kind, request.Output)
		}
	}
}

func TestOpenAPIRenderExamplesKeepCatalogOrder(t *testing.T) {
	encoded, err := json.Marshal(openAPIDocument)
	if err != nil {
		t.Fatal(err)
	}
	previous := -1
	for _, kind := range model.SupportedTypes() {
		index := bytes.Index(encoded, []byte(`"`+kind+`":{"description"`))
		if index < 0 {
			t.Fatalf("example %q is missing from the document", kind)
		}
		if index < previous {
			t.Fatalf("example %q is out of catalogue order", kind)
		}
		previous = index
	}
}

func TestOpenAPIRenderExamplesCarryRenderableAssets(t *testing.T) {
	examples := renderExamplesFromDocument(t)
	avatars := 0
	mapPixels := 0
	enderShulker := false
	inventoryHelmet := false
	for _, kind := range model.SupportedTypes() {
		request := decodeExample(t, kind, examples[kind].Value)
		data, err := request.DecodeData()
		if err != nil {
			t.Fatalf("example %q has invalid data: %v", kind, err)
		}
		switch value := data.(type) {
		case *model.SkinData:
			assertSkinTexture(t, kind, value.Texture)
		case *model.PlayerInventoryData:
			assertSkinTexture(t, kind, value.Skin.Texture)
			assertASCIIText(t, kind+".title", value.Title)
			for _, slot := range value.Armor {
				if slot.Item.ID == "minecraft:diamond_helmet" {
					inventoryHelmet = true
				}
			}
		case *model.PlayerListData:
			assertASCIIText(t, kind+".header", value.Header, value.Footer)
			for _, player := range value.Players {
				assertASCIIText(t, kind+".player", player.Display, player.Name)
				if player.Texture != "" {
					assertSkinTexture(t, kind+".player", player.Texture)
					avatars++
				}
			}
		case *model.TooltipData:
			assertStyledText(t, kind+".title", value.Title)
			assertStyledText(t, kind+".lines", value.Lines...)
		case *model.ContainerData:
			assertASCIIText(t, kind+".title", value.Title)
			for _, slot := range value.Slots {
				if slot.Item.ID == "minecraft:shulker_box" && kind == model.TypeEnderchest {
					enderShulker = true
				}
			}
		case *model.AchievementData:
			assertASCIIText(t, kind+".text", value.Title, value.Description)
		case *model.MapData:
			if len(value.Pixels) == 0 && value.PixelData == "" {
				t.Fatalf("example %q renders no pixels", kind)
			}
			mapPixels = len(value.Pixels)
		}
	}
	if avatars == 0 {
		t.Fatal("playerlist example does not demonstrate an avatar texture")
	}
	if mapPixels != 128*128 {
		t.Fatalf("map example has %d pixels, want %d", mapPixels, 128*128)
	}
	if !enderShulker {
		t.Fatal("enderchest example does not demonstrate a shulker box")
	}
	if !inventoryHelmet {
		t.Fatal("playerInventory example does not demonstrate a diamond helmet")
	}
}

func TestOrderedObjectPreservesInsertionOrder(t *testing.T) {
	encoded, err := json.Marshal(orderedObject{{key: "b", value: 1}, {key: "a", value: map[string]any{"z": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"b":1,"a":{"z":1}}` {
		t.Fatalf("encoded = %s", encoded)
	}
	if _, err := json.Marshal(orderedObject{{key: "bad", value: make(chan int)}}); err == nil {
		t.Fatal("expected a marshal error for an unsupported value")
	}
}
func TestRenderAcceptsEveryPrefilledExample(t *testing.T) {
	server := testServer(t)
	examples := renderExamplesFromDocument(t)
	for _, kind := range model.SupportedTypes() {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/render", bytes.NewReader(examples[kind].Value))
		request.Header.Set("Content-Type", "application/json")
		record := httptest.NewRecorder()
		server.Handler().ServeHTTP(record, request)
		if record.Code != http.StatusOK {
			t.Fatalf("example %q returned status %d: %s", kind, record.Code, record.Body.String())
		}
		var payload struct {
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		}
		if err := json.Unmarshal(record.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.MimeType != "image/png" || payload.Data == "" || payload.Width == 0 || payload.Height == 0 {
			t.Fatalf("example %q returned an unusable payload: %+v", kind, payload)
		}
	}
}
