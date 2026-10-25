package resources

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
)

func TestExtractZipEntriesRejectsZipSlip(t *testing.T) {
	entries := makeZipEntries(t, map[string]string{"../escape.txt": "nope"})
	target := t.TempDir()
	err := extractZipEntries(entries, target, 10, 1024)
	if err == nil {
		t.Fatal("expected zip slip to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(target), "escape.txt")); statErr == nil {
		t.Fatal("zip slip created file outside target")
	}
}

func TestExtractZipEntriesWritesFiles(t *testing.T) {
	entries := makeZipEntries(t, map[string]string{"assets/test.txt": "ok"})
	target := t.TempDir()
	if err := extractZipEntries(entries, target, 10, 1024); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "assets", "test.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("unexpected content %q", data)
	}
}

// ItemsAdder writes a placeholder CRC and a placeholder uncompressed size into the central directory
// of every entry it generates, while the deflate data is intact. archive/zip refuses such an archive,
// so the extractor reads the stream itself: the files must come out complete.
func TestExtractZipEntriesToleratesPlaceholderMetadata(t *testing.T) {
	files := map[string]string{
		"pack.mcmeta":                              `{"pack":{"pack_format":75}}`,
		"assets/minecraft/items/apple.json":        `{"model":{"type":"minecraft:model","model":"minecraft:item/apple"}}`,
		"assets/minecraft/textures/item/apple.png": strings.Repeat("texture", 64),
	}
	archive := patchCentralDirectory(t, zipBytes(t, files), 0, 1337)
	entries := zipEntriesOf(t, archive)
	if !archiveZipRejects(t, entries) {
		t.Fatal("the rewritten central directory was not read as broken by archive/zip, the test proves nothing")
	}
	target := t.TempDir()
	if err := extractZipEntries(entries, target, 10, 1<<20); err != nil {
		t.Fatalf("the placeholder metadata must not block the extraction: %v", err)
	}
	for name, content := range files {
		data, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(data) != content {
			t.Fatalf("%s = %q, want %q", name, data, content)
		}
	}
}

// A CRC that is declared and wrong still means the entry is corrupt: tolerating placeholder metadata
// must not turn the integrity check off.
func TestExtractZipEntriesRejectsABadChecksum(t *testing.T) {
	entries := zipEntriesOf(t, patchCentralDirectory(t, zipBytes(t, map[string]string{"a.txt": "hello"}), 0xdeadbeef, 5))
	target := t.TempDir()
	err := extractZipEntries(entries, target, 10, 1024)
	if err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("err = %v, want a corrupt entry", err)
	}
	if _, statErr := os.Stat(filepath.Join(target, "a.txt")); statErr == nil {
		t.Fatal("a corrupt entry must not be published")
	}
}

// A declared size that pretends to be small must not truncate the entry: the limit is enforced on
// the bytes actually written.
func TestExtractZipEntriesIgnoresAnUnderstatedSize(t *testing.T) {
	content := strings.Repeat("x", 512)
	entries := zipEntriesOf(t, patchCentralDirectory(t, zipBytes(t, map[string]string{"a.txt": content}), 0, 1))
	target := t.TempDir()
	if err := extractZipEntries(entries, target, 10, 1024); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("the entry was truncated to %d bytes", len(data))
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AddonEnabled = false
	return cfg
}

func TestValidatePackMetadataRejectsMalformedAndOutOfRangeFormats(t *testing.T) {
	root := t.TempDir()
	metadata := filepath.Join(root, "pack.mcmeta")
	if err := os.WriteFile(metadata, []byte(`{"pack":{"pack_format":0}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := validatePackMetadata(root); err == nil {
		t.Fatal("expected incompatible pack format error")
	}
	if err := os.WriteFile(metadata, []byte(`{"pack":`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := validatePackMetadata(root); err == nil {
		t.Fatal("expected malformed pack metadata error")
	}
}

func TestValidatePackMetadataAcceptsModernFormatRange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pack.mcmeta"), []byte(`{"pack":{"min_format":[46,1],"max_format":46}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	format, err := validatePackMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	if format != 46 {
		t.Fatalf("pack format = %d, want 46", format)
	}
}

func writeZip(path string, files map[string]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			file.Close()
			return createErr
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			file.Close()
			return writeErr
		}
	}
	if err = writer.Close(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// zipBytes builds an archive in memory.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func makeZipEntries(t *testing.T, files map[string]string) []*zip.File {
	t.Helper()
	return zipEntriesOf(t, zipBytes(t, files))
}

func zipEntriesOf(t *testing.T, archive []byte) []*zip.File {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	return reader.File
}

// patchCentralDirectory rewrites the CRC and the uncompressed size of every central directory entry,
// leaving the local headers and the deflate streams untouched — exactly the shape of an ItemsAdder
// generated pack.
func patchCentralDirectory(t *testing.T, archive []byte, checksum, size uint32) []byte {
	t.Helper()
	patched := append([]byte(nil), archive...)
	offset, count := centralDirectory(t, patched)
	cursor := offset
	for index := 0; index < count; index++ {
		if binary.LittleEndian.Uint32(patched[cursor:]) != 0x02014b50 {
			t.Fatalf("central directory entry %d has no signature at %d", index, cursor)
		}
		binary.LittleEndian.PutUint32(patched[cursor+16:], checksum)
		binary.LittleEndian.PutUint32(patched[cursor+24:], size)
		nameLength := int(binary.LittleEndian.Uint16(patched[cursor+28:]))
		extraLength := int(binary.LittleEndian.Uint16(patched[cursor+30:]))
		commentLength := int(binary.LittleEndian.Uint16(patched[cursor+32:]))
		cursor += 46 + nameLength + extraLength + commentLength
	}
	return patched
}

// centralDirectory finds the end of central directory record and answers its offset and entry count.
func centralDirectory(t *testing.T, archive []byte) (int, int) {
	t.Helper()
	for index := len(archive) - 22; index >= 0; index-- {
		if binary.LittleEndian.Uint32(archive[index:]) != 0x06054b50 {
			continue
		}
		count := int(binary.LittleEndian.Uint16(archive[index+10:]))
		offset := int(binary.LittleEndian.Uint32(archive[index+16:]))
		return offset, count
	}
	t.Fatal("no end of central directory record")
	return 0, 0
}

// archiveZipRejects reports whether reading through archive/zip fails, which is what tells the test
// that the patched archive really reproduces the failure being fixed.
func archiveZipRejects(t *testing.T, entries []*zip.File) bool {
	t.Helper()
	for _, entry := range entries {
		reader, err := entry.Open()
		if err != nil {
			return true
		}
		_, err = io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return true
		}
	}
	return false
}
