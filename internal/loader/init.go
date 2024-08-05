package loader

import (
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/buger/jsonparser"
	log "github.com/sirupsen/logrus"
)

func Init() {
	loadSupportVersions()
}

func loadSupportVersions() {
	bytes, err := tools.Get(url.ICDAVersions)
	if err != nil {
		log.Fatalf("Can't get support versions: %v", err)
	}
	log.Debugf("Load support versions: %s", string(bytes))

	jsonparser.ArrayEach(bytes, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		log.Info(jsonparser.GetString(value))
	}, "versions")
}
