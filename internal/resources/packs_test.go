package resources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
)

// packDir returns the directory the test config reads its packs from, created so a test can drop an
// archive in before the manager ever runs.
func packDir(t *testing.T, cfg config.Config) string {
	t.Helper()
	directory := cfg.PackDir()
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	return directory
}

// writePack writes one archive whose content differs per name, so two packs never share the content
// addressed cache entry by accident.
func writePack(t *testing.T, directory, name string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(directory, name)
	files := map[string]string{
		"pack.mcmeta": `{"pack":{"pack_format":46}}`,
		"assets/minecraft/textures/item/" + name + ".txt": name,
	}
	if err := writeZip(path, files); err != nil {
		t.Fatal(err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func packFiles(report PackReport) []string {
	names := make([]string, 0, len(report.Packs))
	for _, pack := range report.Packs {
		names = append(names, pack.File)
	}
	return names
}

// The first scan follows the client's idea of "creation order": modification time first, then the
// file name, which is what makes dropping a batch of archives in predictable.
func TestPackStoreOrdersArchivesByCreationTimeThenName(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writePack(t, directory, "b.zip", base.Add(2*time.Hour))
	writePack(t, directory, "a.zip", base)
	writePack(t, directory, "c.zip", base.Add(2*time.Hour))

	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	got := packFiles(store.Report())
	want := []string{"a.zip", "b.zip", "c.zip"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	report := store.Report()
	if report.Packs[0].Priority != 1 || report.Packs[2].Priority != 3 {
		t.Fatalf("priorities = %d..%d, want 1..3", report.Packs[0].Priority, report.Packs[2].Priority)
	}
	if report.Packs[0].Status != PackLoaded || report.Packs[0].Root == "" {
		t.Fatalf("pack = %+v, want a loaded pack with an extracted root", report.Packs[0])
	}
	if _, err := os.Stat(filepath.Join(report.Packs[0].Root, "assets", "minecraft", "textures", "item", "a.zip.txt")); err != nil {
		t.Fatalf("the archive was not unzipped: %v", err)
	}
	if roots := store.Roots(); len(roots) != 3 {
		t.Fatalf("roots = %v, want three", roots)
	}
}

// An explicit order outlives the process: the manifest is the only place it lives.
func TestPackStorePersistsAnExplicitOrder(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	writePack(t, directory, "a.zip", time.Time{})
	writePack(t, directory, "b.zip", time.Time{})

	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetOrder([]string{"b.zip", "a.zip"}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := packFiles(manager.PackReport())
	if len(got) != 2 || got[0] != "b.zip" || got[1] != "a.zip" {
		t.Fatalf("order after restart = %v, want [b.zip a.zip]", got)
	}
	if roots := manager.PackRoots(); len(roots) != 2 {
		t.Fatalf("roots = %v, want both packs", roots)
	}
}

// A pack that appears later is appended: dropping an archive in must never silently change which of
// the packs already there wins.
func TestPackStoreAppendsANewArchiveAtTheEnd(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	writePack(t, directory, "a.zip", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	// An older modification time does not let the new archive jump the queue.
	writePack(t, directory, "b.zip", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := store.Reload(false); err != nil {
		t.Fatal(err)
	}
	got := packFiles(store.Report())
	if len(got) != 2 || got[0] != "a.zip" || got[1] != "b.zip" {
		t.Fatalf("order = %v, want [a.zip b.zip]", got)
	}
}

func TestPackStoreRejectsAnIncompleteOrder(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	writePack(t, directory, "a.zip", time.Time{})
	writePack(t, directory, "b.zip", time.Time{})
	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	for name, order := range map[string][]string{
		"missing":   {"a.zip"},
		"unknown":   {"a.zip", "c.zip"},
		"duplicate": {"a.zip", "a.zip"},
		"empty":     {"a.zip", ""},
	} {
		if _, err := store.SetOrder(order); err == nil {
			t.Fatalf("%s: expected an error for %v", name, order)
		}
	}
	// The failed attempts must not have reordered anything.
	if got := packFiles(store.Report()); got[0] != "a.zip" || got[1] != "b.zip" {
		t.Fatalf("order = %v, want it unchanged", got)
	}
}

// One archive the client would refuse must not take the rest of the stack down: it is reported and
// left out of the roots, and the good pack still renders.
func TestPackStoreReportsABrokenArchiveAndKeepsTheRest(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	writePack(t, directory, "good.zip", time.Time{})
	if err := writeZip(filepath.Join(directory, "broken.zip"), map[string]string{"assets/minecraft/textures/item/x.txt": "no metadata"}); err != nil {
		t.Fatal(err)
	}
	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	report := store.Report()
	if len(report.Packs) != 2 {
		t.Fatalf("packs = %+v, want both archives listed", report.Packs)
	}
	var broken Pack
	for _, pack := range report.Packs {
		if pack.File == "broken.zip" {
			broken = pack
		}
	}
	if broken.Status != PackFailed || broken.Error == "" {
		t.Fatalf("broken pack = %+v, want status failed with a reason", broken)
	}
	if roots := store.Roots(); len(roots) != 1 {
		t.Fatalf("roots = %v, want only the readable pack", roots)
	}
	// The order contract still covers the failed entry, so echoing the report back keeps working.
	if _, err := store.SetOrder([]string{"broken.zip", "good.zip"}); err != nil {
		t.Fatalf("the report must be a valid order: %v", err)
	}
}

// A restart costs one hash per archive: the content addressed cache is reused, not rebuilt.
func TestPackStoreReusesTheContentAddressedCache(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	writePack(t, directory, "a.zip", time.Time{})
	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	root := store.Report().Packs[0].Root
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reload(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("the cache was extracted again: %v", err)
	}
	if store.Report().Packs[0].Root != root {
		t.Fatalf("root changed from %q to %q", root, store.Report().Packs[0].Root)
	}
}

// Only visible .zip files are packs. Everything else is reported once instead of being guessed at,
// because a directory of a pack or a stray note is easy to mistake for a pack that silently did
// nothing.
func TestPackStoreIgnoresForeignEntries(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	writePack(t, directory, "a.zip", time.Time{})
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("hi"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".hidden.zip"), []byte("not a zip"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "unpacked"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "unpacked", "pack.mcmeta"), []byte(`{"pack":{"pack_format":46}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	report := store.Report()
	if got := packFiles(report); len(got) != 1 || got[0] != "a.zip" {
		t.Fatalf("packs = %v, want only a.zip", got)
	}
	joined := ""
	for _, warning := range report.Warnings {
		joined += warning + "\n"
	}
	if !strings.Contains(joined, "notes.txt") || !strings.Contains(joined, "unpacked") {
		t.Fatalf("warnings = %v, want both foreign entries reported", report.Warnings)
	}
	if strings.Contains(joined, ".hidden.zip") {
		t.Fatalf("hidden entries must be skipped silently: %v", report.Warnings)
	}
}

// The watcher is what makes dropping an archive in work without a restart. It waits for one stable
// poll before reloading, so a file that is still being copied is never handed to the extractor.
func TestPackStoreWatchPicksUpANewArchive(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go store.Watch(ctx, 10*time.Millisecond)
	writePack(t, directory, "late.zip", time.Time{})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := packFiles(store.Report()); len(got) == 1 && got[0] == "late.zip" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the watcher did not pick up the archive: %+v", store.Report())
}
