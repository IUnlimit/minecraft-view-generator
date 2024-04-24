package global

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/emirpasic/gods/maps/hashmap"
)

// ParentPath perp files draw
const ParentPath = "./config"

const AssetsPath = ParentPath + "/assets"

// Config perpetua config.yml
var Config *model.Config

// ResourceMap map[string]*Resource
var ResourceMap *hashmap.Map
