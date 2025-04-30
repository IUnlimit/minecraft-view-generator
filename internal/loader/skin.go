package loader

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/buger/jsonparser"
	"github.com/emirpasic/gods/maps/hashmap"
	log "github.com/sirupsen/logrus"
)

const (
	SkinSuffix = ".png"
)

func LoadLocalSkins() {
	if !tools.FileExists(global.SkinsPath) {
		_ = os.MkdirAll(global.SkinsPath, os.ModePerm)
		log.Infof("Init skins folder, %s", global.SkinsPath)
	}

	skinMap := hashmap.New()
	err := filepath.WalkDir(global.SkinsPath, func(path string, d os.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		lastIndex := strings.LastIndex(path, string(os.PathSeparator))
		fileName := path[lastIndex+1:]
		fileName = fileName[:len(fileName)-len(SkinSuffix)]
		splits := strings.Split(fileName, "#")
		skin, err := LoadSkinByFile(path, splits[1] == "true")
		if err != nil {
			log.Warnf("Load skin path(%s) failed, %v", path, err)
			return nil
		}
		skinMap.Put(splits[0], skin)
		return nil
	})
	if err != nil {
		log.Errorf("Failed to walk dir(%s), %v", global.SkinsPath, err)
	}
	log.Infof("Success load %d player skins", skinMap.Size())
	global.SkinMap = skinMap
}

// LoadSkinByName load the player skin texture by name
//
//	if use blessing skin, the uuid should not be empty
//
// TODO 加锁, 同一时间同一uuid/name/url 仅允许一个下载
func LoadSkinByName(name string, uuid string, cache bool) (*draw.Skin, error) {
	if cache && len(uuid) > 0 {
		if skin, found := global.SkinMap.Get(uuid); found {
			return skin.(*draw.Skin), nil
		}
	}

	skin, err := func() (*draw.Skin, error) {
		body, err := tools.Get(url.InfoUUID.Format(name))
		if err != nil {
			return nil, err
		}
		_uuid, err := jsonparser.GetString(body, "id")
		if err != nil {
			return nil, err
		}

		if cache {
			if skin, found := global.SkinMap.Get(_uuid); found {
				return skin.(*draw.Skin), nil
			}
		}
		return loadSkinByUUID(_uuid, cache)
	}()
	if skin != nil {
		return skin, nil
	}
	if len(uuid) == 0 {
		return nil, err
	}

	// 第三方皮肤站获取
	for _, skinUrl := range global.Config.Minecraft.BlessingSkin {
		format := url.GetSkin.Format(skinUrl, name)
		s, err := LoadSkinByUrl(format, uuid, false)
		if s != nil {
			global.SkinMap.Put(uuid, s)
			return s, nil
		}
		log.Warnf("Try to get player(name: %s, uuid: %s) skin failed from %s, %v", name, uuid, format, err)
	}
	return nil, err
}

// loadSkinByUUID uuid like 57876712e6a64cb2ad6419137f209beb
func loadSkinByUUID(uuid string, cache bool) (*draw.Skin, error) {
	body, err := tools.Get(url.InfoTexture.Format(uuid))
	if err != nil {
		return nil, err
	}
	errChan := make(chan error, 1)
	skinChan := make(chan *draw.Skin, 1)
	_, err = jsonparser.ArrayEach(body, func(value []byte, _ jsonparser.ValueType, _ int, err error) {
		val, _, _, err := jsonparser.Get(value, "value")
		if err != nil {
			log.Errorf("Failed to parse profile(uuid: %s) properties, %v", uuid, err)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(string(val))
		if err != nil {
			log.Errorf("Failed to decode profile(uuid: %s) value, %v", uuid, err)
			return
		}
		skinUrl, err := jsonparser.GetString(decoded, "textures", "SKIN", "url")
		if err != nil {
			log.Errorf("Failed to decode textures(uuid: %s) url, %v", uuid, err)
			return
		}
		skinModel, err := jsonparser.GetString(decoded, "textures", "SKIN", "metadata", "model")
		if err != nil {
			skinModel = "" // only Alex has metadata.model
		}
		skin, err := LoadSkinByUrl(skinUrl, uuid, skinModel == "slim")
		errChan <- err
		skinChan <- skin
	}, "properties")
	if err != nil {
		return nil, err
	}
	err = <-errChan
	if err != nil {
		return nil, err
	}
	skin := <-skinChan
	if cache {
		global.SkinMap.Put(uuid, skin)
	}
	return skin, nil
}

func LoadSkinByUrl(u string, uuid string, slim bool) (*draw.Skin, error) {
	skinPath := global.SkinPath(fmt.Sprintf("%s#%t", uuid, slim), SkinSuffix)
	err := tools.MakeParentDirs(skinPath)
	if err != nil {
		return nil, err
	}
	err = tools.DownloadFile(skinPath, u, true, false)
	if err != nil {
		return nil, err
	}
	return LoadSkinByFile(skinPath, slim)
}

func LoadSkinByFile(skinPath string, slim bool) (*draw.Skin, error) {
	skin, err := draw.NewSkin(skinPath, slim)
	if err != nil {
		return nil, err
	}
	return skin, nil
}
