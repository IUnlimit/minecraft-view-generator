package global

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/emirpasic/gods/lists/singlylinkedlist"
)

// ParentPath perp files draw
const ParentPath = "./config"

const AssetsPath = ParentPath + "/assets"

func VersionPath(name string) string {
	return AssetsPath + "/" + name
}

// Config perpetua config.yml
var Config *model.Config

var SupportVersionList *singlylinkedlist.List
