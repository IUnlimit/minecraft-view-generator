package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	log "github.com/sirupsen/logrus"
)

func Init() {
	releaseMap = LoadVersions()
	enableVersions := global.Config.Minecraft.Version.Enable
	rMap := LoadResourceMap(enableVersions)
	json, _ := rMap.ToJSON()
	log.Debugf("%s", json)

	rMap.Each(initTextures)
}

func initTextures(_ interface{}, v interface{}) {
	resource := v.(*Resource)
	tMap, err := draw.Map2DTextures(draw.ItemTextures, resource.RootPath)
	if err != nil {
		log.Fatal(err)
	}
	tMap.Get("")
	//log.Info(pretty.Sprint(tMap.Values()))
}
