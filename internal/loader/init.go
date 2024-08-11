package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
)

func Init() {
	FetchSupportVersions()
	version := global.Config.Minecraft.Version
	if hasDefaultVersion(version) {
		LoadResourceList(version.EntryList)
	}
	LoadFont()
	LoadLocalSkins()
}
