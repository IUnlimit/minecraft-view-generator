package tools

import (
	"fmt"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/buger/jsonparser"
	"github.com/schollz/progressbar/v3"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
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

func DownloadFile(filePath string, url string, recover bool, showBar bool) error {
	if !recover && FileExists(filePath) {
		return nil
	}
	err := MakeParentDirs(filePath)
	if err != nil {
		return err
	}

	var out io.Writer
	_ = os.Remove(filePath)
	out, err = os.Create(filePath)
	defer out.(*os.File).Close()
	if err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if showBar {
		bar := progressbar.DefaultBytes(
			resp.ContentLength,
			fmt.Sprintf("Downloading %s", path.Base(filePath)),
		)
		out = io.MultiWriter(out, bar)
	}
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

// TryDownloadClient return need-recover(bool)
func TryDownloadClient(folderPath string, fileName string, info *model.ClientInfo) (bool, error) {
	filePath := folderPath + "/" + fileName
	if FileExists(filePath) {
		sha1, _ := CalculateSHA1(filePath)
		if info.SHA1 == sha1 {
			log.Debugf("File %s exists, sha1 verification passed", filePath)
			return false, nil
		}
		log.Debugf("File %s sha1 verification failed, download again", filePath)
		_ = os.Remove(filePath)
	}
	err := os.MkdirAll(folderPath, os.ModePerm)
	if err != nil {
		return true, err
	}
	err = DownloadFile(filePath, info.Url, true, true)
	if err != nil {
		return true, err
	}
	return true, nil
}
