package resources

import (
	"encoding/json"
	"testing"
)

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
