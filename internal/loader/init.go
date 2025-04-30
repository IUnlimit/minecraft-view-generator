package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
)

func Init() {
	FetchSupportVersions()
	config := global.Config.Minecraft
	version := config.Version
	if setDefaultVersion(version) {
		LoadResourceList(version.EntryList)
	}
	LoadLangVersionMap(config.Resource.Language, version.EntryList)
	LoadFont(config.Resource.Font)
	LoadLocalSkins()
	// TODO check config-version & texture-version
}
