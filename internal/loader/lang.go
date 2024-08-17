package loader

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/pkg/lang"
	log "github.com/sirupsen/logrus"
)

func LoadLangVersionMap(langName string, entryList []*model.Entry) {
	successList := make([]string, 0)
	for _, entry := range entryList {
		err := lang.Load(entry.Name, langName)
		if err != nil {
			log.Errorf("Failed to load %s (version %s) language file, skipped, %v", langName, entry.Name, err)
			continue
		}
		successList = append(successList, entry.Name)
	}
	log.Infof("Success load language file %s (versions %s)", langName, successList)
}
