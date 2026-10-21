package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/custom"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/renderer"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	server, _ := testServerWithAdapter(t)
	return server
}

func testServerWithAdapter(t *testing.T) (*Server, *recordingAdapter) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.CustomContentsDir = filepath.Join(cfg.DataDir, "contents")
	cfg.CustomItemsCache = filepath.Join(cfg.CustomContentsDir, "items_ids_cache.yml")
	cfg.RequireResources = false
	cfg.AllowFallbackRenderer = true
	manager, err := resources.NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &recordingAdapter{inner: renderer.PrimitiveAdapter{}}
	compositor := renderer.NewCompositor(adapter)
	compositor.MaxWidth = cfg.MaxImageWidth
	compositor.MaxHeight = cfg.MaxImageHeight
	catalog := custom.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewServer(cfg, manager, compositor, catalog, slog.New(slog.NewTextHandler(io.Discard, nil))), adapter
}

// recordingAdapter keeps the resource context of the last render, which is how a test asserts what
// the renderer was actually handed.
type recordingAdapter struct {
	inner renderer.PrimitiveAdapter
	last  resources.Context
}

func (a *recordingAdapter) record(resource resources.Context) {
	a.last = resource
}

func (a *recordingAdapter) Backend() string { return a.inner.Backend() }

func (a *recordingAdapter) RenderItem(ctx context.Context, resource resources.Context, item model.Item, size int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderItem(ctx, resource, item, size)
}

func (a *recordingAdapter) RenderPlayer(ctx context.Context, resource resources.Context, skin model.SkinData, equipment renderer.Equipment, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderPlayer(ctx, resource, skin, equipment, width, height)
}

func (a *recordingAdapter) RenderTooltip(ctx context.Context, resource resources.Context, data model.TooltipData, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderTooltip(ctx, resource, data, width, height)
}

func (a *recordingAdapter) RenderContainer(ctx context.Context, resource resources.Context, kind string, data model.ContainerData, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderContainer(ctx, resource, kind, data, width, height)
}

func (a *recordingAdapter) RenderPlayerInventory(ctx context.Context, resource resources.Context, data model.PlayerInventoryData, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderPlayerInventory(ctx, resource, data, width, height)
}

func (a *recordingAdapter) RenderPlayerList(ctx context.Context, resource resources.Context, data model.PlayerListData, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderPlayerList(ctx, resource, data, width, height)
}

func (a *recordingAdapter) RenderMap(ctx context.Context, resource resources.Context, data model.MapData, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderMap(ctx, resource, data, width, height)
}

func (a *recordingAdapter) RenderAchievement(ctx context.Context, resource resources.Context, data model.AchievementData, width, height int) (image.Image, error) {
	a.record(resource)
	return a.inner.RenderAchievement(ctx, resource, data, width, height)
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

// assertRenderableText allows everything the provider chain draws: ASCII from the bitmap sheets and
// the CJK blocks from the Unifont archive, which is what the Chinese examples rely on.
func assertRenderableText(t *testing.T, label string, values ...string) {
	t.Helper()
	for _, value := range values {
		for _, char := range value {
			if isRenderableRune(char) {
				continue
			}
			t.Fatalf("%s contains %q in %q, which the font cannot draw", label, char, value)
		}
	}
}

func isRenderableRune(char rune) bool {
	if char >= 32 && char <= 126 {
		return true
	}
	// Unifont covers the CJK blocks and the full width punctuation around them; anything else is
	// either a formatting code or a code point the renderer would draw as the missing box.
	return char >= 0x2000 && char <= 0xFFEF
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
			if !isRenderableRune(char) {
				t.Fatalf("%s contains %q in %q, which the font cannot draw", label, char, value)
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
			assertRenderableText(t, kind+".title", value.Title)
			for _, slot := range value.Armor {
				if slot.Item.ID == "minecraft:diamond_helmet" {
					inventoryHelmet = true
				}
			}
		case *model.PlayerListData:
			assertRenderableText(t, kind+".header", value.Header, value.Footer)
			for _, player := range value.Players {
				assertRenderableText(t, kind+".player", player.Display, player.Name)
				if player.Texture != "" {
					assertSkinTexture(t, kind+".player", player.Texture)
					avatars++
				}
			}
		case *model.TooltipData:
			assertStyledText(t, kind+".title", value.Title)
			assertStyledText(t, kind+".lore", value.Item.Lore...)
		case *model.ContainerData:
			assertRenderableText(t, kind+".title", value.Title)
			for _, slot := range value.Slots {
				if slot.Item.ID == "minecraft:shulker_box" && kind == model.TypeEnderchest {
					enderShulker = true
				}
			}
		case *model.AchievementData:
			assertRenderableText(t, kind+".text", value.Title, value.Description)
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

// The resources endpoint has to name every directory and file a render reads, including the font
// assets of the prepared versions and the custom content index.
func TestSourcesReportsEveryResourcePath(t *testing.T) {
	server := testServer(t)
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, httptest.NewRequest(http.MethodGet, "/api/v1/resources/sources", nil))
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", record.Code, record.Body.String())
	}
	var report struct {
		DataDir  string `json:"dataDir"`
		Renderer struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"renderer"`
		Versions       []map[string]any `json:"versions"`
		CustomContents struct {
			Root       string         `json:"root"`
			ItemsCache string         `json:"itemsCache"`
			Status     string         `json:"status"`
			Items      map[string]int `json:"items"`
		} `json:"customContents"`
	}
	if err := json.Unmarshal(record.Body.Bytes(), &report); err != nil {
		t.Fatalf("sources body: %v", err)
	}
	if report.DataDir != server.cfg.DataDir {
		t.Fatalf("dataDir = %q, want %q", report.DataDir, server.cfg.DataDir)
	}
	if report.Renderer.Name == "" || report.Renderer.Status == "" {
		t.Fatalf("renderer = %+v", report.Renderer)
	}
	if report.Versions == nil {
		t.Fatal("versions is null, it has to be an array")
	}
	if report.CustomContents.Root != server.cfg.CustomContentsDir || report.CustomContents.ItemsCache != server.cfg.CustomItemsCache {
		t.Fatalf("customContents = %+v", report.CustomContents)
	}
	if report.CustomContents.Status != custom.StatusReady {
		t.Fatalf("customContents status = %q, want ready for an empty index", report.CustomContents.Status)
	}
	if report.CustomContents.Items["cached"] != 0 {
		t.Fatalf("customContents items = %v, want an empty index", report.CustomContents.Items)
	}
}

// An operator who drops a pack into the contents directory or edits the index calls reload instead of
// restarting the service, and the report he gets back is the same segment the sources endpoint shows.
func TestReloadRebuildsTheCustomIndex(t *testing.T) {
	server := testServer(t)
	writeCustomPack(t, server.cfg)

	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, httptest.NewRequest(http.MethodPost, "/api/v1/resources/custom/reload", nil))
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", record.Code, record.Body.String())
	}
	var payload struct {
		CustomContents custom.Sources `json:"customContents"`
	}
	if err := json.Unmarshal(record.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CustomContents.Items.Cached != 1 || payload.CustomContents.Items.WithDefinition != 1 {
		t.Fatalf("index = %+v, want the single indexed item", payload.CustomContents.Items)
	}
	if len(payload.CustomContents.Packs) != 1 || payload.CustomContents.Packs[0].Name != "fishing_pack" {
		t.Fatalf("packs = %+v", payload.CustomContents.Packs)
	}

	sources := httptest.NewRecorder()
	server.Handler().ServeHTTP(sources, httptest.NewRequest(http.MethodGet, "/api/v1/resources/sources", nil))
	if !strings.Contains(sources.Body.String(), "fishing_pack") {
		t.Fatalf("sources does not report the pack: %s", sources.Body.String())
	}
}

// A customData value that the index resolves travels to the renderer as a model plus inline textures,
// from every item position the request can name, and an unknown value leaves the request alone.
func TestRenderResolvesCustomArtForEveryItemPosition(t *testing.T) {
	server, adapter := testServerWithAdapter(t)
	writeCustomPack(t, server.cfg)
	for _, payload := range []string{
		`{"version":"1.21.11","type":"item","output":"base64","data":{"id":"minecraft:paper","customData":1134}}`,
		`{"version":"1.21.11","type":"tooltip","output":"base64","data":{"item":{"id":"minecraft:paper","customData":1134,"name":"Starfish"}}}`,
		`{"version":"1.21.11","type":"inventory","output":"base64","data":{"title":"Chest","rows":3,"columns":9,"slots":[{"slot":0,"item":{"id":"minecraft:paper","customData":1134}}]}}`,
		`{"version":"1.21.11","type":"achievement","output":"base64","data":{"title":"Task","icon":{"id":"minecraft:paper","customData":1134}}}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		record := httptest.NewRecorder()
		server.Handler().ServeHTTP(record, request)
		if record.Code != http.StatusOK {
			t.Fatalf("status = %d for %s: %s", record.Code, payload, record.Body.String())
		}
		art, resolved := adapter.last.CustomArt[model.CustomArtKey("minecraft:paper", 1134)]
		if !resolved {
			t.Fatalf("the renderer was not handed the custom art for %s", payload)
		}
		if art.Model == nil || art.Model["parent"] != "minecraft:item/generated" {
			t.Fatalf("model = %+v, want the generated parent", art.Model)
		}
		if !strings.HasPrefix(art.Textures["fishing_pack:item/starfish"], "data:image/png;base64,") {
			t.Fatalf("textures = %+v, want an inlined PNG", art.Textures)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"base64","data":{"id":"minecraft:paper","customData":999999}}`))
	request.Header.Set("Content-Type", "application/json")
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK {
		t.Fatalf("an unknown customData must still render: %d %s", record.Code, record.Body.String())
	}
	if len(adapter.last.CustomArt) != 0 {
		t.Fatalf("an unknown customData was resolved: %+v", adapter.last.CustomArt)
	}
}

// The prefilled examples have to keep demonstrating the font and the magnification this service is
// able to do, so they are asserted to carry Chinese and a scale option.
func TestPrefilledExamplesShowChineseAndMagnification(t *testing.T) {
	examples := renderExamplesFromDocument(t)
	tooltipPayload := string(examples[model.TypeTooltip].Value)
	if !strings.ContainsAny(tooltipPayload, "中文深海剑字形点阵") {
		t.Fatalf("the tooltip example does not demonstrate Chinese: %s", tooltipPayload)
	}
	if !strings.Contains(tooltipPayload, `"scale"`) {
		t.Fatalf("the tooltip example does not demonstrate magnification: %s", tooltipPayload)
	}
	if !strings.Contains(string(examples[model.TypeItem].Value), `"scale"`) {
		t.Fatalf("the item example does not demonstrate magnification: %s", examples[model.TypeItem].Value)
	}
}

// writeCustomPack installs one fishing pack and its index into the configured contents directory.
func writeCustomPack(t *testing.T, cfg config.Config) {
	t.Helper()
	files := map[string]string{
		"fishing_pack/configs/fishing.yml": `info:
  namespace: fishing_pack
items:
  starfish:
    resource:
      material: PAPER
      generate: false
      model_path: item/starfish
`,
		"fishing_pack/resourcepack/fishing_pack/models/item/starfish.json": `{"parent":"minecraft:item/generated","textures":{"layer0":"fishing_pack:item/starfish"}}`,
		"items_ids_cache.yml": "PAPER:\n  fishing_pack:starfish: 1134\n",
	}
	for name, content := range files {
		path := filepath.Join(cfg.CustomContentsDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	texture := filepath.Join(cfg.CustomContentsDir, "fishing_pack/resourcepack/fishing_pack/textures/item/starfish.png")
	if err := os.MkdirAll(filepath.Dir(texture), 0o755); err != nil {
		t.Fatal(err)
	}
	sheet := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			sheet.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	file, err := os.Create(texture)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, sheet); err != nil {
		t.Fatal(err)
	}
}
