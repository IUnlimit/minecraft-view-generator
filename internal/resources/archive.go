package resources

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractZipEntries(entries []*zip.File, target string, maxFiles int, maxBytes int64, open func(*zip.File) (io.ReadCloser, error)) error {
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
		entrySize := int64(entry.UncompressedSize64)
		if entry.UncompressedSize64 > uint64(maxBytes) || total > maxBytes-entrySize {
			return fmt.Errorf("archive exceeds uncompressed size limit")
		}
		total += entrySize
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		reader, err := open(entry)
		if err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(destination), ".extract-*")
		if err != nil {
			reader.Close()
			return err
		}
		_, copyErr := io.CopyN(tmp, reader, int64(entry.UncompressedSize64))
		closeReaderErr := reader.Close()
		closeFileErr := tmp.Close()
		if copyErr == io.EOF && entry.UncompressedSize64 == 0 {
			copyErr = nil
		}
		if copyErr != nil {
			os.Remove(tmp.Name())
			return copyErr
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
