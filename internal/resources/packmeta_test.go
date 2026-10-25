package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePackMeta(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pack.mcmeta"), []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return root
}

func findOverlay(metadata packMetadata, directory string) (packOverlay, bool) {
	for _, overlay := range metadata.Overlays {
		if overlay.Directory == directory {
			return overlay, true
		}
	}
	return packOverlay{}, false
}

// 1.21.9+ spells every bound as a [major, minor] tuple and widens max_format far past anything a
// current client reports. Neither may cost the pack its place in the stack.
func TestReadPackMetadataAcceptsTupleBounds(t *testing.T) {
	root := writePackMeta(t, `{"pack":{"description":"Default HD","pack_format":15,"min_format":[15,0],"max_format":[1000,0],"supported_formats":[15,1000]}}`)
	metadata, err := readPackMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.compatible() {
		t.Fatalf("range = %v, a pack that supports 15 must be compatible", metadata)
	}
	if format := metadata.packFormat(); format != 15 {
		t.Fatalf("packFormat = %d, want 15", format)
	}
	if min, max := metadata.formatRange(); min != 15 || max != 1000 {
		t.Fatalf("range = %d-%d, want 15-1000", min, max)
	}
}

// itemsAdderPackMetadata is the pack.mcmeta of the ItemsAdder generated pack, verbatim: pack_format
// 75, a supported_formats pair that is narrower than its bounds and ten overlays.
const itemsAdderPackMetadata = `{"pack":{"pack_format":75,"supported_formats":[32,64],"min_format":32,"max_format":9999,"description":"ItemsAdder"},"overlays":{"entries":[{"directory":"ia_overlay_1_20_5_plus","formats":[32,64],"min_format":32,"max_format":87},{"directory":"ia_overlay_1_21_2_plus","formats":[42,64],"min_format":42,"max_format":87},{"directory":"ia_overlay_1_21_4_to_5","formats":[46,55],"min_format":46,"max_format":55},{"directory":"ia_overlay_1_21_4_plus","formats":[46,64],"min_format":46,"max_format":87},{"directory":"ia_overlay_1_21_5","formats":[55,55],"min_format":55,"max_format":55},{"directory":"ia_overlay_1_21_6_to_11","formats":[63,64],"min_format":63,"max_format":75},{"directory":"ia_overlay_1_21_6_plus","formats":[63,64],"min_format":63,"max_format":87},{"directory":"ia_overlay_modern_atlas","formats":[75,87],"min_format":75,"max_format":87},{"directory":"ia_overlay_26_1_plus","formats":[84,87],"min_format":84,"max_format":87},{"directory":"ia_overlay_26_2_plus","formats":[88,9999],"min_format":88,"max_format":9999}]}}`

func TestReadPackMetadataReadsTheItemsAdderOverlays(t *testing.T) {
	metadata, err := readPackMetadata(writePackMeta(t, itemsAdderPackMetadata))
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.compatible() {
		t.Fatal("the ItemsAdder pack supports 1.21.11 and must be compatible")
	}
	if format := metadata.packFormat(); format != 75 {
		t.Fatalf("packFormat = %d, want 75", format)
	}
	if min, max := metadata.formatRange(); min != 32 || max != 9999 {
		t.Fatalf("range = %d-%d, want 32-9999", min, max)
	}
	if len(metadata.Overlays) != 10 {
		t.Fatalf("overlays = %d, want 10", len(metadata.Overlays))
	}
	for _, want := range []packOverlay{
		{Directory: "ia_overlay_1_20_5_plus", Min: 32, Max: 87},
		{Directory: "ia_overlay_1_21_4_to_5", Min: 46, Max: 55},
		{Directory: "ia_overlay_1_21_6_to_11", Min: 63, Max: 75},
		{Directory: "ia_overlay_modern_atlas", Min: 75, Max: 87},
		{Directory: "ia_overlay_26_2_plus", Min: 88, Max: 9999},
	} {
		got, ok := findOverlay(metadata, want.Directory)
		if !ok {
			t.Fatalf("overlay %q is missing", want.Directory)
		}
		if got.Min != want.Min || got.Max != want.Max {
			t.Fatalf("overlay %q = %d-%d, want %d-%d", want.Directory, got.Min, got.Max, want.Min, want.Max)
		}
	}
	// The client format decides which of them stack: six of the ten cover format 75.
	selected := 0
	for _, overlay := range metadata.Overlays {
		if clientPackFormat >= overlay.Min && clientPackFormat <= overlay.Max {
			selected++
		}
	}
	if selected != 6 {
		t.Fatalf("%d overlays cover format %d, want 6", selected, clientPackFormat)
	}
}

// Before 1.21.9 the interval was an object, and a pack that still uses it has to load the same way.
func TestReadPackMetadataAcceptsTheObjectFormatsSpelling(t *testing.T) {
	metadata, err := readPackMetadata(writePackMeta(t, `{"pack":{"pack_format":46,"supported_formats":{"min_inclusive":32,"max_inclusive":46}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if min, max := metadata.formatRange(); min != 32 || max != 46 {
		t.Fatalf("range = %d-%d, want 32-46", min, max)
	}
	if format := metadata.packFormat(); format != 46 {
		t.Fatalf("packFormat = %d, want 46", format)
	}
}

// An overlay resolves its own interval, and falls back to the pack's range when it declares none.
func TestReadPackMetadataResolvesOverlayRanges(t *testing.T) {
	metadata, err := readPackMetadata(writePackMeta(t, `{"pack":{"pack_format":46},"overlays":{"entries":[
		{"directory":"from-formats","formats":[40,50]},
		{"directory":"from-pack"},
		{"directory":"from-bounds","formats":[1,2],"min_format":46,"max_format":50},
		{"directory":""}
	]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Overlays) != 3 {
		t.Fatalf("overlays = %+v, want the unnamed entry skipped", metadata.Overlays)
	}
	for _, want := range []packOverlay{
		{Directory: "from-formats", Min: 40, Max: 50},
		{Directory: "from-pack", Min: 46, Max: 46},
		{Directory: "from-bounds", Min: 46, Max: 50},
	} {
		got, ok := findOverlay(metadata, want.Directory)
		if !ok {
			t.Fatalf("overlay %q is missing", want.Directory)
		}
		if got.Min != want.Min || got.Max != want.Max {
			t.Fatalf("overlay %q = %d-%d, want %d-%d", want.Directory, got.Min, got.Max, want.Min, want.Max)
		}
	}
}

// A pack that declares no pack_format still reports the lower bound of the range it does declare.
func TestReadPackMetadataReportsTheLowerBoundWithoutPackFormat(t *testing.T) {
	metadata, err := readPackMetadata(writePackMeta(t, `{"pack":{"min_format":[46,1],"max_format":[100,0]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if format := metadata.packFormat(); format != 46 {
		t.Fatalf("packFormat = %d, want 46", format)
	}
}

// Only a pack whose entire range lies above what this renderer understands is refused.
func TestReadPackMetadataRefusesAFuturePack(t *testing.T) {
	for name, content := range map[string]string{
		"pack_format": `{"pack":{"pack_format":200}}`,
		"range":       `{"pack":{"min_format":[200,0],"max_format":[250,0]}}`,
		"supported":   `{"pack":{"supported_formats":[101,200]}}`,
	} {
		metadata, err := readPackMetadata(writePackMeta(t, content))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if metadata.compatible() {
			t.Fatalf("%s: a pack for a future version must be refused (%+v)", name, metadata)
		}
	}
}

func TestReadPackMetadataRejectsMalformedMetadata(t *testing.T) {
	for name, content := range map[string]string{
		"broken json":  `{"pack":`,
		"zero format":  `{"pack":{"pack_format":0}}`,
		"bad tuple":    `{"pack":{"pack_format":[15]}}`,
		"bad range":    `{"pack":{"supported_formats":[15]}}`,
		"bad override": `{"pack":{"pack_format":46},"overlays":{"entries":[{"directory":"a","min_format":"x"}]}}`,
	} {
		if _, err := readPackMetadata(writePackMeta(t, content)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	root := t.TempDir()
	if _, err := readPackMetadata(root); err == nil || !strings.Contains(err.Error(), "pack.mcmeta") {
		t.Fatalf("a pack without pack.mcmeta must be refused, got %v", err)
	}
}
