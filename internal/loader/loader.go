package loader

import (
	"errors"
	"fmt"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/buger/jsonparser"
	"github.com/emirpasic/gods/lists/singlylinkedlist"
	log "github.com/sirupsen/logrus"
	path2 "path"
	"sync"
)

func FetchSupportVersions() {
	bytes, err := tools.Get(url.ICDAVersions)
	if err != nil {
		log.Fatalf("Can't get support versions, %v", err)
	}
	log.Debugf("Load support versions(%s)", string(bytes))

	supports := singlylinkedlist.New()
	_, err = jsonparser.ArrayEach(bytes, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		version, err := jsonparser.GetUnsafeString(value)
		if err != nil {
			log.Warnf("Parse version failed, %v", err)
			return
		}
		supports.Add(version)
	}, "versions")
	if err != nil {
		log.Errorf("Failed to for-each version list, %v", err)
		return
	}
	global.SupportVersionList = supports
}

func hasDefaultVersion(version *model.Version) bool {
	entryList := version.EntryList
	if entryList == nil || len(entryList) == 0 {
		// select newest version
		value, ok := global.SupportVersionList.Get(0)
		if !ok {
			log.Warnf("Can't find default version resource, skipped ...")
			return false
		}
		entryList = make([]*model.Entry, 0)
		version.EntryList = append(entryList, &model.Entry{
			Name: value.(string),
			Hash: "",
		})
	}
	return true
}

func LoadResourceList(list []*model.Entry) {
	for _, entry := range list {
		go func() {
			err := LoadResource(entry)
			if err != nil {
				log.Errorf("Can't get resource map, version(%s) load skipped, %v", entry.Name, err)
			}
		}()
	}
}

func LoadResource(entry *model.Entry) error {
	bytes, err := tools.Get(fmt.Sprintf(url.ICDAResource, entry.Name))
	if err != nil {
		return err
	}
	if tools.IsResponseError(bytes) {
		return errors.New(string(bytes))
	}
	hash, err := jsonparser.GetString(bytes, "hash")
	if err != nil {
		return err
	}
	log.Infof("Start loading version(%s) with hash(%s)", entry.Name, hash)

	err = doDownloadedEntries(entry.Name, bytes)
	if err != nil {
		return err
	}

	entry.Hash = hash
	// update config
	return nil
}

func doDownloadedEntries(versionName string, bytes []byte) error {
	var wg sync.WaitGroup
	err := jsonparser.ObjectEach(bytes, func(key []byte, value []byte, _ jsonparser.ValueType, _ int) error {
		downloadUrl := string(key)
		path := global.VersionPath(versionName) + string(value) + "/" + path2.Base(downloadUrl)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := tools.DownloadFile(path, downloadUrl, false)
			if err != nil {
				log.Warnf("Download file to path(%s) failed with url(%s), %v", path, downloadUrl, err)
			}
		}()
		return nil
	}, "downloaded-entries")
	if err != nil {
		return err
	}
	wg.Wait()
	log.Infof("Resource version(%s) download entries success", versionName)
	return nil
}
