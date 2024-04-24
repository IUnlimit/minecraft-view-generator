package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	log "github.com/sirupsen/logrus"
)

func Init() {
	releaseMap = LoadVersions()
	enableVersions := global.Config.Minecraft.Version.Enable
	global.ResourceMap = LoadResourceList(enableVersions)
	json, _ := global.ResourceMap.ToJSON()
	log.Debugf("%s", json)
}
