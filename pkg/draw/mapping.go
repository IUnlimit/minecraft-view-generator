package draw

import (
	"github.com/emirpasic/gods/maps/hashmap"
	log "github.com/sirupsen/logrus"
	"io/fs"
	"path/filepath"
)

func Map2DTextures(class string, rootPath string) (*hashmap.Map, error) {
	textureMap := hashmap.New()
	path := rootPath + class
	err := filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		// TODO 步进至文件夹 / 后缀文件,自动合并资源
		if info.IsDir() {
			log.Debugf("Unexpect dir path: %s", path)
			return nil
		}
		textureMap.Put(info.Name(), &Assets{
			Name: info.Name(),
			Path: path,
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return textureMap, nil
}
