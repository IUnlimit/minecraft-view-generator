package tools

import (
	"fmt"
	"github.com/buger/jsonparser"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
)

func Get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpect response code: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func IsResponseError(body []byte) bool {
	errorStr, err := jsonparser.GetString(body, "error")
	if err != nil && len(errorStr) == 0 {
		return false
	}
	log.Debugf("Request failed with body: %s", string(body))
	return true
}

func DownloadFile(filePath string, url string, recover bool) error {
	if !recover && FileExists(filePath) {
		return nil
	}
	err := MakeParentDirs(filePath)
	if err != nil {
		return err
	}

	_ = os.Remove(filePath)
	out, err := os.Create(filePath)
	defer out.Close()
	if err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

func MakeParentDirs(filePath string) error {
	dir := filepath.Dir(filePath)
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create parent directories: %v", err)
	}
	return nil
}
