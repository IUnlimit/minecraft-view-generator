package loader

import "github.com/IUnlimit/minecraft-view-generator/internal/model"

type Resource struct {
	// 资源元数据
	MetaData *model.Release
	// 资源根路径
	RootPath string
}
