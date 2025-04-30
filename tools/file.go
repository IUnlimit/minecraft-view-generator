package tools

import (
	"archive/zip"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func CreateFile(filePath string, data []byte) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

// FileExists check file or folder exists
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	// there are some issues with os.IsExist
	return !os.IsNotExist(err)
}

// CalculateSHA1 Calculate file SHA1 checksum
func CalculateSHA1(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha1.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	hashInBytes := hash.Sum(nil)
	return fmt.Sprintf("%x", hashInBytes), nil
}

// UnzipSpecEntity extract specific draw in jar to dest directory
func UnzipSpecEntity(src string, specPath string, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, specPath) {
			continue
		}
		targetPath := filepath.Join(dest, strings.TrimPrefix(f.Name, specPath))

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(targetPath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.Create(targetPath)
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)
		if err != nil {
			return err
		}
		_ = outFile.Close()
		_ = rc.Close()
	}
	return nil
}
