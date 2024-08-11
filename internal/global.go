package global

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/emirpasic/gods/maps/hashmap"
)

// ParentPath perp files draw
const ParentPath = "./config"

const AssetsPath = ParentPath + "/assets"

const FontsPath = ParentPath + "/fonts/Minecraft.ttf"

const SkinsPath = ParentPath + "/skins"

func VersionPath(version string) string {
	return AssetsPath + "/" + version
}

func SkinPath(uuid string, suffix string) string {
	return SkinsPath + "/" + uuid + suffix
}

// Config mvg config.yml
var Config *model.Config

// LatestVersion use for default
var LatestVersion string

// VersionMap id(string): release(*model.Release)
var VersionMap *hashmap.Map

// SkinMap uuid: *draw.Skin
var SkinMap *hashmap.Map
