package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
)

func Init() {
	FetchSupportVersions()
	config := global.Config.Minecraft
	version := config.Version
	if hasDefaultVersion(version) {
		LoadResourceList(version.EntryList)
	}
	LoadFont(config.Resource.Font)
	LoadLocalSkins()
}
