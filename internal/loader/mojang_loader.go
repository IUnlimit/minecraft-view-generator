package loader

import (
	"encoding/json"
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

func fetchMojangVersions() (*hashmap.Map, error) {
	data, err := tools.Get(url.VersionManifest.Format())
	if err != nil {
		return nil, err
	}

	m := hashmap.New()
	latestRelease, _ := jsonparser.GetString(data, "latest", "release")
	_, _ = jsonparser.ArrayEach(data, func(per []byte, _ jsonparser.ValueType, _ int, err error) {
		typeName, err := jsonparser.GetString(per, "type")
		if err != nil {
			log.Warnf("Parse Mojang version failed, %v", err)
			return
		}
		if typeName != "release" {
			return
		}

		id, err := jsonparser.GetString(per, "id")
		if err != nil {
			log.Warnf("Parse mojang version failed, %v", err)
			return
		}

		downloadURL, err := jsonparser.GetString(per, "url")
		if err != nil {
			log.Warnf("Parse mojang version failed, %v", err)
			return
		}
		m.Put(id, &model.Release{
			ID:             id,
			PackageInfoURL: downloadURL,
		})
	}, "versions")
	log.Infof("Successfully loaded %d mojang releases, latest version: %s(Release)", m.Size(), latestRelease)
	global.LatestVersion = latestRelease
	return m, nil
}

func loadMojangResource(version string) error {
	get, ok := global.VersionMap.Get(version)
	if !ok {
		return fmt.Errorf("version %s not found", version)
	}
	release := get.(*model.Release)

	bytes, err := tools.Get(release.PackageInfoURL)
	if err != nil {
		return err
	}

	value, _, _, err := jsonparser.Get(bytes, "downloads", "client")
	if err != nil {
		return err
	}
	var clientInfo model.ClientInfo
	err = json.Unmarshal(value, &clientInfo)
	if err != nil {
		return err
	}
	release.ClientInfo = &clientInfo

	folderPath := global.VersionPath(version) // ./config/assets/{version}
	fileName := "client.jar"
	log.Infof("Start loading mojang client(%s)", version)
	needRecover, err := tools.TryDownloadClient(folderPath, fileName, &clientInfo)
	if err != nil {
		return err
	}

	assetsFolder := "assets"
	filePath := folderPath + "/" + fileName
	unzipFolder := folderPath + "/" + assetsFolder
	if !needRecover && tools.FileExists(unzipFolder) {
		log.Infof("Detect assets folder, no need to unzip client")
		return nil
	}

	_ = os.Remove(unzipFolder)
	err = tools.UnzipSpecEntity(filePath, assetsFolder, unzipFolder)
	if err != nil {
		return err
	}
	log.Infof("Extract mojang assets success")
	return nil
}
