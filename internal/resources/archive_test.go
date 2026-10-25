package resources

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
)

func TestExtractZipEntriesRejectsZipSlip(t *testing.T) {
	entries := makeZipEntries(t, map[string]string{"../escape.txt": "nope"})
	target := t.TempDir()
	err := extractZipEntries(entries, target, 10, 1024, func(entry *zip.File) (io.ReadCloser, error) { return entry.Open() })
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
	if err := extractZipEntries(entries, target, 10, 1024, func(entry *zip.File) (io.ReadCloser, error) { return entry.Open() }); err != nil {
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

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AddonEnabled = false
	return cfg
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

func makeZipEntries(t *testing.T, files map[string]string) []*zip.File {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return reader.File
}
