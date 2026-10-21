package resources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
)

// The client loads the Unifont definition and its bitmap archive from the asset index, where the
// jar only has an empty placeholder. The overlay must land them under assets/ and let the index win.
func TestMaterializeFontAssetsOverlaysTheIndex(t *testing.T) {
	dataDir := t.TempDir()
	objects := filepath.Join(dataDir, "objects")
	if err := os.MkdirAll(objects, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(hash, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(objects, hash), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	write("aaaa", "unifont-archive")
	write("bbbb", `{"providers":[{"type":"unihex"}]}`)
	write("cccc", "not a font")
	root := filepath.Join(dataDir, "resources", "1.21.11")
	target := filepath.Join(root, "vanilla")
	stale := filepath.Join(target, "assets", "minecraft", "font", "include", "unifont.json")
	if err := os.MkdirAll(filepath.Dir(stale), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte(`{"providers":[]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	index := AssetIndex{Objects: map[string]AssetObject{
		"minecraft/font/unifont.zip":          {Hash: "aaaa"},
		"minecraft/font/include/unifont.json": {Hash: "bbbb"},
		"minecraft/sounds/random/click.ogg":   {Hash: "cccc"},
	}}
	if err := materializeFontAssets(root, index, dataDir); err != nil {
		t.Fatal(err)
	}
	if !fontAssetsPresent(root) {
		t.Fatal("font archive was not materialized")
	}
	definition, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(definition) != `{"providers":[{"type":"unihex"}]}` {
		t.Fatalf("the index definition did not replace the jar placeholder: %s", definition)
	}
	if _, err = os.Stat(filepath.Join(target, "assets", "minecraft", "sounds", "random", "click.ogg")); err == nil {
		t.Fatal("non font assets must not be copied into the resource tree")
	}
	manifest, err := os.ReadFile(filepath.Join(root, fontOverlayManifest))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "assets/minecraft/font/unifont.zip") {
		t.Fatalf("the overlay manifest does not list the archive: %s", manifest)
	}
}

// A tree that only carries the client jar ships a font directory too, and the Unifont archive alone
// is what the old check accepted. Both have to be rejected: only the manifest names every file the
// overlay wrote, so a partially restored tree still renders missing glyphs for CJK.
func TestFontAssetsPresentRejectsPartialTrees(t *testing.T) {
	if fontAssetsPresent(t.TempDir()) {
		t.Fatal("an empty tree must not report font assets")
	}
	root := t.TempDir()
	archive := filepath.Join(root, "vanilla", "assets", "minecraft", "font", "unifont.zip")
	if err := os.MkdirAll(filepath.Dir(archive), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("zip"), 0o640); err != nil {
		t.Fatal(err)
	}
	if fontAssetsPresent(root) {
		t.Fatal("a tree without the overlay manifest must not report font assets")
	}
	if err := os.WriteFile(filepath.Join(root, fontOverlayManifest), []byte(`{"files":["assets/minecraft/font/unifont.zip","assets/minecraft/font/include/unifont.json"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if fontAssetsPresent(root) {
		t.Fatal("a tree with a listed file missing must not report font assets")
	}
	definition := filepath.Join(root, "vanilla", "assets", "minecraft", "font", "include", "unifont.json")
	if err := os.MkdirAll(filepath.Dir(definition), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(definition, []byte(`{"providers":[]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if !fontAssetsPresent(root) {
		t.Fatal("a tree with every listed file must report font assets")
	}
	_ = os.Remove(archive)
	if fontAssetsPresent(root) {
		t.Fatal("a tree whose archive was removed must not report font assets")
	}
}

// A ready state points at a tree, not at its contents, so a tree published by a binary that predates
// the overlay is repaired from the object cache on the next start instead of being trusted.
func TestNewManagerRestoresTheFontOverlayOfReadyTrees(t *testing.T) {
	dataDir := t.TempDir()
	root := legacyFontTree(t, dataDir)
	cfg := config.Default()
	cfg.DataDir = dataDir
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !fontAssetsPresent(root) {
		t.Fatal("startup did not restore the font overlay")
	}
	state, ok := manager.State("1.21.11")
	if !ok || state.Status != StatusReady || state.Error != "" {
		t.Fatalf("unexpected state after repair: %+v", state)
	}
	if _, ready := manager.Context("1.21.11"); !ready {
		t.Fatal("a repaired tree must be renderable")
	}
}

// When the object cache is gone as well the tree cannot be repaired, and the version stops being
// ready: rendering has to answer 409 instead of a picture full of missing glyphs.
func TestNewManagerFlagsReadyTreesWhoseFontObjectsAreGone(t *testing.T) {
	dataDir := t.TempDir()
	legacyFontTree(t, dataDir)
	for _, name := range []string{"aaaa", "bbbb"} {
		if err := os.Remove(filepath.Join(dataDir, "objects", name)); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.DataDir = dataDir
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := manager.State("1.21.11")
	if !ok || state.Status != StatusFailed || !strings.Contains(state.Error, "font assets") {
		t.Fatalf("unexpected state after a failed repair: %+v", state)
	}
	if _, ready := manager.Context("1.21.11"); ready {
		t.Fatal("an unrepairable tree must not be renderable")
	}
}

// legacyFontTree writes the pieces a prepare run leaves behind when it predates the overlay: the
// stored asset index, the jar font files and a ready state — but no Unifont asset at all.
func legacyFontTree(t *testing.T, dataDir string) string {
	t.Helper()
	objects := filepath.Join(dataDir, "objects")
	if err := os.MkdirAll(objects, 0o750); err != nil {
		t.Fatal(err)
	}
	for hash, content := range map[string]string{"aaaa": "unifont-archive", "bbbb": `{"providers":[]}`} {
		if err := os.WriteFile(filepath.Join(objects, hash), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dataDir, "resources", "1.21.11")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	index, err := json.Marshal(AssetIndex{Objects: map[string]AssetObject{
		"minecraft/font/unifont.zip":          {Hash: "aaaa"},
		"minecraft/font/include/unifont.json": {Hash: "bbbb"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "official-assets-index.json"), index, 0o640); err != nil {
		t.Fatal(err)
	}
	// The jar ships default.json and include/default.json, so a legacy tree is full of textures and
	// font definitions and still has no glyph beyond Latin.
	for _, name := range []string{"default.json", "include/default.json", "include/space.json"} {
		file := filepath.Join(root, "vanilla", "assets", "minecraft", "font", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(`{"providers":[]}`), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	states := filepath.Join(dataDir, "states")
	if err := os.MkdirAll(states, 0o750); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(VersionState{Version: "1.21.11", Status: StatusReady, Stage: "ready", Progress: 1, ResourceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(states, "1.21.11.json"), state, 0o640); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAddonMetadataAcceptsEntryObjectsAndPaths(t *testing.T) {
	var metadata AddonMetadata
	payload := []byte(`{"hash":"abc","client-entries":[{"path":"assets/a","url":"https://example.invalid/a","sha1":"00112233445566778899aabbccddeeff00112233"}],"downloaded-entries":["assets/b"]}`)
	if err := json.Unmarshal(payload, &metadata); err != nil {
		t.Fatal(err)
	}
	entries := metadata.Entries()
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].Hash != "00112233445566778899aabbccddeeff00112233" || entries[1].Path != "assets/b" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestWithMinecraftVersionPreservesQuery(t *testing.T) {
	value, err := withMinecraftVersion("https://api.example.invalid/versions?format=json", "1.21.4")
	if err != nil {
		t.Fatal(err)
	}
	if value != "https://api.example.invalid/versions?format=json&minecraftVersion=1.21.4" {
		t.Fatalf("unexpected URL %q", value)
	}
}
