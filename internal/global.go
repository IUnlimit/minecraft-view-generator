package global

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/emirpasic/gods/maps/hashmap"
)

// ParentPath perp files draw
const ParentPath = "./config"

const AssetsPath = ParentPath + "/assets"

func VersionPath(version string) string {
	return AssetsPath + "/" + version
}

// Config perpetua config.yml
var Config *model.Config

// LatestVersion use for default
var LatestVersion string

// VersionMap id(string): release(*model.Release)
var VersionMap *hashmap.Map
