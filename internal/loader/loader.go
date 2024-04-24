package loader

import (
	"encoding/json"
	"errors"
	"fmt"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/buger/jsonparser"
	"github.com/emirpasic/gods/maps/hashmap"
	log "github.com/sirupsen/logrus"
	"os"
)

var releaseMap map[string]*model.Release

// LoadVersions 加载版本信息
func LoadVersions() map[string]*model.Release {
	data, err := tools.Get(url.VersionManifest)
	if err != nil {
		log.Panic(err)
	}

	rMap := make(map[string]*model.Release)
	latestRelease, _ := jsonparser.GetString(data, "latest", "release")
	latestSnapshot, _ := jsonparser.GetString(data, "latest", "snapshot")
	_, _ = jsonparser.ArrayEach(data, func(per []byte, dataType jsonparser.ValueType, offset int, err error) {
		typeName, _ := jsonparser.GetString(per, "type")
		if typeName != "release" {
			return
		}
		id, _ := jsonparser.GetString(per, "id")
		downloadURL, _ := jsonparser.GetString(per, "url")
		rMap[id] = &model.Release{
			ID:            id,
			ClientInfoURL: downloadURL,
		}
	}, "versions")
	log.Infof("Successfully loaded %d releases, latest version: %s(Release) %s(Snapshot)", len(rMap), latestRelease, latestSnapshot)
	return rMap
}

// LoadResourceList 加载资源版本列表
func LoadResourceList(versions []string) *hashmap.Map {
	rMap := hashmap.New()
	for _, version := range versions {
		resource, err := LoadResource(version)
		if err != nil {
			log.Errorf("Version '%s' failed to load: %v", version, err)
			continue
		}
		rMap.Put(version, resource)
	}
	log.Infof("Successfully loaded %d versions, current: %s", rMap.Size(), rMap.Keys())
	return rMap
}

// LoadResource 加载版本对应的资源
func LoadResource(version string) (*Resource, error) {
	if _, ok := releaseMap[version]; !ok {
		return nil, errors.New(fmt.Sprintf("unknown version: %s", version))
	}
	release := releaseMap[version]
	resource, err := loadAssets(release)
	if err != nil {
		return nil, err
	}
	return resource, nil
}

// loadAssets 加载指定版本的资源文件
func loadAssets(release *model.Release) (*Resource, error) {
	bytes, err := tools.Get(release.ClientInfoURL)
	if err != nil {
		return nil, err
	}

	value, _, _, err := jsonparser.Get(bytes, "downloads", "client")
	if err != nil {
		return nil, err
	}
	var clientInfo model.ClientInfo
	err = json.Unmarshal(value, &clientInfo)
	if err != nil {
		return nil, err
	}
	release.ClientInfo = &clientInfo

	folderPath := fmt.Sprintf("%s/%s", global.AssetsPath, release.ID)
	fileName := "client.jar"
	err = tools.TryDownloadClient(folderPath, fileName, &clientInfo)
	if err != nil {
		return nil, err
	}

	assetsFolder := "assets"
	err = extractAsset(folderPath, assetsFolder, fileName)
	if err != nil {
		return nil, err
	}
	return &Resource{
		MetaData: release,
		RootPath: folderPath,
	}, nil
}

func extractAsset(folderPath string, assetsFolder string, fileName string) error {
	filePath := folderPath + "/" + fileName
	fileFolder := folderPath + "/" + assetsFolder
	_ = os.Remove(fileFolder)
	err := tools.UnzipSpecEntity(filePath, assetsFolder, fileFolder)
	if err != nil {
		return err
	}
	return nil
}
