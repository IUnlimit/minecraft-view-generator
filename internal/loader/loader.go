package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	log "github.com/sirupsen/logrus"
	"sync"
)

func FetchSupportVersions() {
	mojangVMap, err := fetchMojangVersions()
	if err != nil {
		log.Fatalf("Fetch mojang versions failed, %v", err)
	}
	icdaVList, err := fetchICDAVersions()
	if err != nil {
		log.Fatalf("Fetch ICDA versions failed, %v", err)
	}

	for key := range mojangVMap.Keys() {
		found := icdaVList.Contains(key)
		if !found {
			mojangVMap.Remove(key)
		}
	}
	json, _ := mojangVMap.ToJSON()
	log.Debugf("Load support VersionMap: %s", json)
	global.VersionMap = mojangVMap
}

func hasDefaultVersion(version *model.Version) bool {
	entryList := version.EntryList
	if entryList == nil || len(entryList) == 0 {
		entryList = make([]*model.Entry, 0)
		version.EntryList = append(entryList, &model.Entry{
			Name: global.LatestVersion,
			Hash: "",
		})
	}
	return true
}

func LoadResourceList(list []*model.Entry) {
	var wg sync.WaitGroup
	for _, entry := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := LoadResource(entry)
			if err != nil {
				log.Errorf("Can't get resource map, version(%s) load skipped, %v", entry.Name, err)
			}
		}()
	}
	wg.Wait()
}

func LoadResource(entry *model.Entry) error {
	err := loadMojangResource(entry.Name)
	if err != nil {
		return err
	}
	err = loadICDAResource(entry)
	if err != nil {
		return err
	}
	return nil
}
