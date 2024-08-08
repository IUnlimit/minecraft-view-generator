package conf

import (
	"embed"
	"github.com/IUnlimit/minecraft-view-generator/configs"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"os"
)

// LoadConfig creat and load config, return exists(file)
func LoadConfig(fileName string, fileFolder string, fs embed.FS, config any) (bool, error) {
	filePath := fileFolder + fileName
	exists := tools.FileExists(filePath)
	if !exists {
		log.Warnf("Can't find `%s`, generating default configuration", fileName)
		data, err := fs.ReadFile(fileName)
		if err != nil {
			return false, err
		}
		err = os.MkdirAll(fileFolder, os.ModePerm)
		if err != nil {
			return false, err
		}
		err = tools.CreateFile(filePath, data)
		if err != nil {
			return false, err
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return exists, err
	}

	err = yaml.Unmarshal(data, config)
	if err != nil {
		return exists, err
	}
	return exists, nil
}

// UpdateConfig update config.yml
func UpdateConfig(config *model.Config) error {
	bytes, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	filePath := global.ParentPath + "/" + configs.ConfigFileName
	err = os.WriteFile(filePath, bytes, 0644)
	if err != nil {
		return err
	}
	return nil
}
