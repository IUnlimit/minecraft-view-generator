package loader

import (
	"fmt"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/buger/jsonparser"
	"github.com/emirpasic/gods/lists/singlylinkedlist"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"sync"
)

func fetchICDAVersions() (*singlylinkedlist.List, error) {
	bytes, err := tools.Get(url.ICDAVersions.Format())
	if err != nil {
		return nil, err
	}

	supports := singlylinkedlist.New()
	_, err = jsonparser.ArrayEach(bytes, func(value []byte, _ jsonparser.ValueType, _ int, err error) {
		version, err := jsonparser.GetUnsafeString(value)
		if err != nil {
			log.Warnf("Parse ICDA version failed, %v", err)
			return
		}
		supports.Add(version)
	}, "versions")
	if err != nil {
		return nil, err
	}
	return supports, nil
}

func loadICDAResource(entry *model.Entry) error {
	bytes, err := tools.Get(url.ICDAResource.Format(entry.Name))
	if err != nil {
		return err
	}
	if tools.IsResponseError(bytes) {
		return fmt.Errorf(string(bytes))
	}
	hash, err := jsonparser.GetString(bytes, "hash")
	if err != nil {
		return err
	}
	if entry.Hash == hash {
		log.Infof("ICDA resource with hash %s has been loaded", hash)
		return nil
	}
	log.Infof("Start loading ICDA resource version(%s) with hash(%s)", entry.Name, hash)

	err = doDownloadedEntries(entry.Name, bytes)
	if err != nil {
		return err
	}
	log.Infof("ICDA Resource version(%s) download entries success", entry.Name)
	err = doRenameEntries(entry.Name, bytes)
	if err != nil {
		return err
	}
	log.Infof("ICDA Resource version(%s) rename entries success", entry.Name)

	entry.Hash = hash
	err = conf.UpdateConfig(global.Config)
	if err != nil {
		log.Warnf("Can't save config file, %v", err)
		return nil
	}
	log.Infof("Updated config file with hash(%s)", hash)
	return nil
}

func doDownloadedEntries(versionName string, bytes []byte) error {
	var wg sync.WaitGroup
	versionPath := global.VersionPath(versionName)
	err := jsonparser.ObjectEach(bytes, func(key []byte, value []byte, _ jsonparser.ValueType, _ int) error {
		downloadUrl := string(key)
		filePath := versionPath + string(value) + "/" + path.Base(downloadUrl)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := tools.DownloadFile(filePath, downloadUrl, true, false)
			if err != nil {
				log.Warnf("Download file to path(%s) failed with url(%s), %v", filePath, downloadUrl, err)
			}
		}()
		return nil
	}, "downloaded-entries")
	if err != nil {
		return err
	}
	wg.Wait()
	return nil
}

func doRenameEntries(versionName string, bytes []byte) error {
	versionPath := global.VersionPath(versionName)
	err := jsonparser.ObjectEach(bytes, func(key []byte, value []byte, _ jsonparser.ValueType, _ int) error {
		sourcePath := versionPath + "/" + string(key)
		targetPath := versionPath + "/" + string(value)
		err := os.Rename(sourcePath, targetPath)
		if err != nil {
			log.Warnf("Move file from '%s' to '%s' failed, %v", sourcePath, targetPath, err)
		}
		return nil
	}, "rename-entries")
	if err != nil {
		return err
	}
	return nil
}
