package custom

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
)

// The parser has to read the three shapes the real packs use: the flat icon pack whose item names are
// numbers, the quoted display names of the fishing pack, and the furniture pack whose resources are
// followed by nested behaviours the item drawing never needs.
func TestParserReadsThePackShapes(t *testing.T) {
	document, warnings := parseDocument([]byte(`
info:
  namespace: fishing_pack
items:
  starfish:
    display_name: "&fStarfish"
    resource:
      material: PAPER
      generate: false
      model_path: item/starfish
      model_id: 1134
  crate:
    display_name: '黑箱子'
    resource:
      material: paper
      generate: true
      textures:
        - item/crate_top
        - 'item/crate_side'
    behaviours:
      furniture:
        solid: true
`))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if namespace := namespaceOf(document); namespace != "fishing_pack" {
		t.Fatalf("namespace = %q, want fishing_pack", namespace)
	}
	items := yamlMapping(document["items"])
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	starfish := yamlMapping(yamlMapping(items["starfish"])["resource"])
	if material := yamlString(starfish["material"]); material != "PAPER" {
		t.Fatalf("material = %q", material)
	}
	if path := yamlString(starfish["model_path"]); path != "item/starfish" {
		t.Fatalf("model_path = %q", path)
	}
	crate := yamlMapping(yamlMapping(items["crate"])["resource"])
	if !yamlBool(crate["generate"]) {
		t.Fatal("generate was not read as true")
	}
	if textures := yamlStrings(crate["textures"]); len(textures) != 2 || textures[1] != "item/crate_side" {
		t.Fatalf("textures = %v", textures)
	}
	if display := yamlString(yamlMapping(items["crate"])["display_name"]); display != "黑箱子" {
		t.Fatalf("display_name = %q", display)
	}
}

// A colon inside a value belongs to the value: the category icon of a pack is a namespaced item id,
// and the cache keys are namespaced ids too.
func TestParserKeepsColonsInsideValues(t *testing.T) {
	document, _ := parseDocument([]byte("icon: icon_pack:1\nPAPER:\n  _iainternal:icon_back_orange: 10005\n"))
	if icon := yamlString(document["icon"]); icon != "icon_pack:1" {
		t.Fatalf("icon = %q, want icon_pack:1", icon)
	}
	paper := yamlMapping(document["PAPER"])
	if len(paper) != 1 {
		t.Fatalf("PAPER entries = %d, want 1", len(paper))
	}
	if value := yamlString(paper["_iainternal:icon_back_orange"]); value != "10005" {
		t.Fatalf("cached value = %q, want 10005", value)
	}
}

// Comments and inline sequences are what the hand written category lists look like.
func TestParserReadsCommentsAndInlineSequences(t *testing.T) {
	document, _ := parseDocument([]byte(`
# leading comment
items:
  one:
    textures: [item/one, item/two] # trailing comment
    material: PAPER
`))
	textures := yamlStrings(yamlMapping(document["items"])["one"].(map[string]any)["textures"])
	if len(textures) != 2 || textures[0] != "item/one" {
		t.Fatalf("inline sequence = %v", textures)
	}
}

// The index is grouped by material because the numbers repeat across materials: PAPER 1134 and
// FISHING_ROD 1134 are different items.
func TestCacheKeysIncludeTheMaterial(t *testing.T) {
	cache, warnings := ParseCache([]byte(`
PAPER:
  fishing_pack:starfish: 1134
  aabaoxiang:toffy_crate_black: 10115
FISHING_ROD:
  fishing_pack:starfish: 1134
  broken: not-a-number
`))
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not a number") {
		t.Fatalf("warnings = %v", warnings)
	}
	if id, ok := cache.Lookup("minecraft:paper", 1134); !ok || id != "fishing_pack:starfish" {
		t.Fatalf("PAPER 1134 = %q, %v", id, ok)
	}
	if id, ok := cache.Lookup("fishing_rod", 1134); !ok || id != "fishing_pack:starfish" {
		t.Fatalf("FISHING_ROD 1134 = %q, %v", id, ok)
	}
	if _, ok := cache.Lookup("minecraft:apple", 1134); ok {
		t.Fatal("an unindexed material resolved an item")
	}
	if cache.Len() != 3 {
		t.Fatalf("indexed entries = %d, want 3", cache.Len())
	}
}

// One pack renders from a model file, one from a texture list, and one keeps its images directly
// under textures/: all three have to reach a drawable model with its textures inlined.
func TestCatalogResolvesEveryPackLayout(t *testing.T) {
	root := t.TempDir()
	contents := filepath.Join(root, "contents")
	writeFile(t, filepath.Join(contents, "fishing_pack/configs/fishing.yml"), `
info:
  namespace: fishing_pack
items:
  starfish:
    resource:
      material: PAPER
      generate: false
      model_path: item/starfish
`)
	writeFile(t, filepath.Join(contents, "fishing_pack/resourcepack/fishing_pack/models/item/starfish.json"),
		`{"parent":"minecraft:item/generated","textures":{"layer0":"fishing_pack:item/starfish"}}`)
	writePNG(t, filepath.Join(contents, "fishing_pack/resourcepack/fishing_pack/textures/item/starfish.png"))

	writeFile(t, filepath.Join(contents, "icon_pack/configs/items.yml"), `
info:
  namespace: icon_pack
items:
  1:
    resource:
      material: PAPER
      generate: true
      textures:
        - item/1
`)
	writePNG(t, filepath.Join(contents, "icon_pack/textures/item/1.png"))

	writeFile(t, filepath.Join(contents, "aabaoxiang/configs/bx/bx.yml"), `
info:
  namespace: aabaoxiang
items:
  crate:
    resource:
      material: paper
      generate: false
      model_path: item/crate
`)
	writeFile(t, filepath.Join(contents, "aabaoxiang/resourcepack/assets/aabaoxiang/models/item/crate.json"),
		`{"credit":"MadewithBlockbench","textures":{"0":"aabaoxiang:bxtp/crate"},"elements":[{"from":[0,0,0],"to":[16,16,16]}]}`)
	writePNG(t, filepath.Join(contents, "aabaoxiang/resourcepack/assets/aabaoxiang/textures/bxtp/crate.png"))

	writeFile(t, filepath.Join(contents, "items_ids_cache.yml"), `
PAPER:
  fishing_pack:starfish: 1134
  icon_pack:1: 11257
  aabaoxiang:crate: 10115
APPLE:
  fishing_pack:starfish: 1
`)

	catalog := New(testConfig(contents), nil)
	sources := catalog.Reload()
	if sources.Status != StatusReady {
		t.Fatalf("status = %q, want ready", sources.Status)
	}
	if len(sources.Packs) != 3 {
		t.Fatalf("packs = %d, want 3", len(sources.Packs))
	}
	if sources.Items.Cached != 4 || sources.Items.WithDefinition != 4 {
		t.Fatalf("items = %+v, want 4 cached entries that all have a definition", sources.Items)
	}

	starfish, reason, ok := catalog.Art("minecraft:paper", 1134)
	if !ok {
		t.Fatalf("the fishing pack item did not resolve: %s", reason)
	}
	if parent := yamlString(starfish.Model["parent"]); parent != "minecraft:item/generated" {
		t.Fatalf("parent = %q, want the vanilla generated model", parent)
	}
	layer := yamlString(yamlMapping(starfish.Model["textures"])["layer0"])
	if layer != "fishing_pack:item/starfish" {
		t.Fatalf("layer0 = %q", layer)
	}
	if encoded := starfish.Textures["fishing_pack:item/starfish"]; !strings.HasPrefix(encoded, "data:image/png;base64,") {
		t.Fatalf("the texture was not inlined: %.40q", encoded)
	}

	icon, reason, ok := catalog.Art("minecraft:paper", 11257)
	if !ok {
		t.Fatalf("the generated icon did not resolve: %s", reason)
	}
	if encoded := icon.Textures["icon_pack:item/1"]; encoded == "" {
		t.Fatal("the icon texture was not inlined")
	}

	crate, reason, ok := catalog.Art("minecraft:paper", 10115)
	if !ok {
		t.Fatalf("the furniture model did not resolve: %s", reason)
	}
	if elements, _ := crate.Model["elements"].([]any); len(elements) != 1 {
		t.Fatalf("the furniture geometry was dropped: %v", crate.Model["elements"])
	}
	if crate.Textures["aabaoxiang:bxtp/crate"] == "" {
		t.Fatal("the furniture texture was not inlined")
	}

	// The same number under another material is a different item, and an unknown number falls back.
	if _, _, ok := catalog.Art("minecraft:apple", 1134); ok {
		t.Fatal("a number that is not indexed for the material resolved an item")
	}
	if _, reason, ok := catalog.Art("minecraft:paper", 999999); ok || !strings.Contains(reason, "items_ids_cache") {
		t.Fatalf("an unknown customData resolved: %q", reason)
	}
	if _, reason, ok := catalog.Art("minecraft:stone", 1134); ok || !strings.Contains(reason, "items_ids_cache") {
		t.Fatalf("an unindexed material resolved: %q", reason)
	}
}

// A custom pack lives outside the resource tree the renderer can read, so the .png.mcmeta of an
// animated sprite has to travel with the inlined texture: without it the renderer cannot tell a
// vertical animation strip from one tall still image, and the face would smear across every frame.
func TestCatalogCarriesTheSpriteMetadata(t *testing.T) {
	contents := filepath.Join(t.TempDir(), "contents")
	writeFile(t, filepath.Join(contents, "pack/configs/items.yml"), `
info:
  namespace: pack
items:
  flame:
    resource:
      material: PAPER
      generate: false
      model_path: item/flame
`)
	writeFile(t, filepath.Join(contents, "pack/resourcepack/assets/pack/models/item/flame.json"),
		`{"textures":{"2":"pack:hammer","1":"pack:animations/animations_2"},"elements":[{"from":[0,0,7],"to":[16,16,9],"faces":{"south":{"texture":"#2","uv":[0,0,16,16]}}}]}`)
	writePNG(t, filepath.Join(contents, "pack/resourcepack/assets/pack/textures/hammer.png"))
	writePNG(t, filepath.Join(contents, "pack/resourcepack/assets/pack/textures/animations/animations_2.png"))
	writeFile(t, filepath.Join(contents, "pack/resourcepack/assets/pack/textures/animations/animations_2.png.mcmeta"),
		`{"animation": {"frametime": 2}}`)
	writeFile(t, filepath.Join(contents, "items_ids_cache.yml"), "PAPER:\n  pack:flame: 1134\n")

	catalog := New(testConfig(contents), nil)
	catalog.Reload()
	art, reason, ok := catalog.Art("minecraft:paper", 1134)
	if !ok {
		t.Fatalf("the animated item did not resolve: %s", reason)
	}
	if art.Textures["pack:animations/animations_2"] == "" {
		t.Fatal("the animated texture was not inlined")
	}
	if meta := art.Metadata["pack:animations/animations_2"]; !strings.Contains(meta, "animation") {
		t.Fatalf("the animation metadata was not carried with the texture: %q", meta)
	}
	if meta, present := art.Metadata["pack:hammer"]; present {
		t.Fatalf("a still texture carries metadata: %q", meta)
	}
	if art.Textures["pack:hammer"] == "" {
		t.Fatal("the still texture was not inlined")
	}
}

// A custom model may inherit from another custom model, which the renderer cannot reach: the catalog
// flattens the chain and keeps only the vanilla parent.
func TestCatalogFlattensCustomParents(t *testing.T) {
	contents := filepath.Join(t.TempDir(), "contents")
	writeFile(t, filepath.Join(contents, "pack/configs/items.yml"), `
info:
  namespace: pack
items:
  inherited:
    resource:
      material: PAPER
      model_path: item/inherited
`)
	writeFile(t, filepath.Join(contents, "pack/resourcepack/assets/pack/models/item/base.json"),
		`{"parent":"minecraft:item/handheld","textures":{"layer0":"pack:item/base"},"display":{"gui":{"scale":[2,2,2]}}}`)
	writeFile(t, filepath.Join(contents, "pack/resourcepack/assets/pack/models/item/inherited.json"),
		`{"parent":"pack:item/base","textures":{"layer0":"pack:item/own"}}`)
	writePNG(t, filepath.Join(contents, "pack/resourcepack/assets/pack/textures/item/own.png"))
	writeFile(t, filepath.Join(contents, "items_ids_cache.yml"), "PAPER:\n  pack:inherited: 7\n")

	catalog := New(testConfig(contents), nil)
	catalog.Reload()
	art, reason, ok := catalog.Art("minecraft:paper", 7)
	if !ok {
		t.Fatalf("the inherited model did not resolve: %s", reason)
	}
	if parent := yamlString(art.Model["parent"]); parent != "minecraft:item/handheld" {
		t.Fatalf("parent = %q, want the vanilla handheld model", parent)
	}
	if layer := yamlString(yamlMapping(art.Model["textures"])["layer0"]); layer != "pack:item/own" {
		t.Fatalf("layer0 = %q, want the child texture", layer)
	}
	if display := yamlMapping(art.Model["display"]); display["gui"] == nil {
		t.Fatal("the display transform of the custom parent was dropped")
	}
}

// An absent contents directory is an empty index, not an error: the service keeps rendering vanilla
// items and only reports the missing index.
func TestCatalogWithoutContentsIsEmpty(t *testing.T) {
	root := t.TempDir()
	contents := filepath.Join(root, "missing")
	catalog := New(testConfig(contents), nil)
	sources := catalog.Sources()
	if sources.Status != StatusReady {
		t.Fatalf("status = %q, want ready", sources.Status)
	}
	if sources.Items.Cached != 0 || sources.CachePresent {
		t.Fatalf("items = %+v, want an empty index", sources.Items)
	}
	if !containsWarning(sources.Warnings, "items_ids_cache.yml") {
		t.Fatalf("warnings = %v, want the missing index to be reported", sources.Warnings)
	}
	if _, err := os.Stat(contents); err != nil {
		t.Fatalf("the contents directory was not created: %v", err)
	}
	if _, _, ok := catalog.Art("minecraft:paper", 1134); ok {
		t.Fatal("an empty index resolved a custom item")
	}
}

// A broken file is skipped with a warning instead of failing the whole index.
func TestCatalogSkipsBrokenConfig(t *testing.T) {
	contents := filepath.Join(t.TempDir(), "contents")
	writeFile(t, filepath.Join(contents, "broken/configs/broken.yml"), "items:\n  nameless:\n    resource:\n      material: PAPER\n")
	writeFile(t, filepath.Join(contents, "good/configs/good.yml"), `
info:
  namespace: good
items:
  item:
    resource:
      material: PAPER
      generate: true
      textures:
        - item/item
`)
	writePNG(t, filepath.Join(contents, "good/textures/item/item.png"))
	writeFile(t, filepath.Join(contents, "items_ids_cache.yml"), "PAPER:\n  good:item: 3\n")

	catalog := New(testConfig(contents), nil)
	sources := catalog.Reload()
	if !containsWarning(sources.Warnings, "info.namespace") {
		t.Fatalf("warnings = %v, want the nameless file to be reported", sources.Warnings)
	}
	if _, _, ok := catalog.Art("minecraft:paper", 3); !ok {
		t.Fatal("the healthy pack was not indexed")
	}
}

func testConfig(contents string) config.Config {
	return config.Config{
		CustomContentsDir:     contents,
		CustomItemsCache:      filepath.Join(contents, "items_ids_cache.yml"),
		MaxCustomItems:        1_000,
		MaxCustomTextureBytes: 1 << 20,
		MaxCustomLayers:       16,
	}
}

func containsWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

// writeFile creates a fixture file and its directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writePNG writes a two by two texture, which is all the catalog needs to inline it.
func writePNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	sheet := image.NewRGBA(image.Rect(0, 0, 2, 2))
	sheet.Set(0, 0, color.RGBA{R: 255, A: 255})
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, sheet); err != nil {
		t.Fatal(err)
	}
}
