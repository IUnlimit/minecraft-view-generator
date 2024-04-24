package tools

import (
	"errors"
	"fmt"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
)

func Get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(fmt.Sprintf("Unexpect response code: %d", resp.StatusCode))
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

type WriteCounter struct {
	Total uint64
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.PrintProgress()
	return n, nil
}

func (wc *WriteCounter) PrintProgress() {
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\rDownloading... %s complete", humanize.Bytes(wc.Total))
}

func TryDownloadClient(folderPath string, fileName string, info *model.ClientInfo) error {
	//log.Debugf("Start dowloading %s ...", url)
	filePath := folderPath + "/" + fileName
	if FileExists(filePath) {
		sha1, _ := CalculateSHA1(filePath)
		if info.SHA1 == sha1 {
			log.Debugf("File %s exists, sha1 verification passed", filePath)
			return nil
		}
		log.Debugf("File %s sha1 verification failed, download again", filePath)
		_ = os.Remove(filePath)
	}
	err := os.MkdirAll(folderPath, os.ModePerm)
	if err != nil {
		return err
	}
	err = DownloadFile(filePath, info.URL)
	if err != nil {
		return err
	}
	return nil
}

func DownloadFile(filePath string, url string) error {
	_ = os.Remove(filePath + ".tmp")
	out, err := os.Create(filePath + ".tmp")
	defer out.Close()
	if err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	counter := &WriteCounter{}
	if _, err = io.Copy(out, io.TeeReader(resp.Body, counter)); err != nil {
		return err
	}
	fmt.Print("\n")
	if err = os.Rename(filePath+".tmp", filePath); err != nil {
		return err
	}
	return nil
}
