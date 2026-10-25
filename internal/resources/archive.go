package resources

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// zipEntryReader opens the raw segment of one entry, without the layer archive/zip puts in front of
// it. entry.Open() must not be used here: it wraps the stream in a checksumReader that enforces the
// uncompressed size and the CRC the central directory declares, so a generator that writes
// placeholders there (ItemsAdder writes 1337 and 0 for every entry of its generated packs) turns a
// perfectly readable archive into "unexpected EOF" and "zip: checksum error". Reading the stored or
// deflated bytes directly keeps the damaged deflate stream detectable while ignoring the sizes, and
// the caller checks the CRC itself.
func zipEntryReader(entry *zip.File) (io.ReadCloser, error) {
	raw, err := entry.OpenRaw()
	if err != nil {
		return nil, fmt.Errorf("archive entry %q: %w", entry.Name, err)
	}
	switch entry.Method {
	case zip.Store:
		return io.NopCloser(raw), nil
	case zip.Deflate:
		return flate.NewReader(raw), nil
	default:
		return nil, fmt.Errorf("archive entry %q uses the unsupported compression method %d", entry.Name, entry.Method)
	}
}

func extractZipEntries(entries []*zip.File, target string, maxFiles int, maxBytes int64) error {
	if len(entries) > maxFiles {
		return fmt.Errorf("archive contains %d files, limit is %d", len(entries), maxFiles)
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		name := filepath.ToSlash(entry.Name)
		if name == "" || name == "." {
			continue
		}
		if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || filepath.IsAbs(filepath.FromSlash(name)) || hasDotDotSegment(name) {
			return fmt.Errorf("archive entry %q escapes target", entry.Name)
		}
		if strings.ContainsRune(name, '\x00') {
			return fmt.Errorf("archive entry contains NUL")
		}
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry %q escapes target", entry.Name)
		}
		destination := filepath.Join(target, clean)
		if !pathWithin(target, destination) {
			return fmt.Errorf("archive entry %q escapes target", entry.Name)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive symlink %q is not allowed", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o750); err != nil {
				return err
			}
			continue
		}
		// The declared uncompressed size is not trusted: some generators write a placeholder there,
		// so the budget is enforced on the bytes actually written, which is what a zip bomb has to
		// spend anyway.
		remaining := maxBytes - total
		if remaining < 0 {
			return fmt.Errorf("archive exceeds uncompressed size limit")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		reader, err := zipEntryReader(entry)
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(destination), ".extract-*")
		if err != nil {
			reader.Close()
			return err
		}
		sum := crc32.NewIEEE()
		written, copyErr := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(reader, remaining+1))
		closeReaderErr := reader.Close()
		closeFileErr := tmp.Close()
		if copyErr != nil {
			os.Remove(tmp.Name())
			return copyErr
		}
		if written > remaining {
			os.Remove(tmp.Name())
			return fmt.Errorf("archive exceeds uncompressed size limit")
		}
		total += written
		// A zero CRC is how a generator spells "not computed", so it is skipped; a declared one is
		// still enforced, which is what keeps a damaged entry from passing silently.
		if entry.CRC32 != 0 && sum.Sum32() != entry.CRC32 {
			os.Remove(tmp.Name())
			return fmt.Errorf("archive entry %q is corrupt", entry.Name)
		}
		if closeReaderErr != nil || closeFileErr != nil {
			os.Remove(tmp.Name())
			if closeReaderErr != nil {
				return closeReaderErr
			}
			return closeFileErr
		}
		if err := os.Chmod(tmp.Name(), 0o640); err != nil {
			os.Remove(tmp.Name())
			return err
		}
		if err := os.Rename(tmp.Name(), destination); err != nil {
			os.Remove(tmp.Name())
			return err
		}
	}
	return nil
}

func hasDotDotSegment(name string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(name), "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}
