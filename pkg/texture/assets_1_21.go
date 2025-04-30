package texture

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"strings"
)

const (
	Icon = Root + "/gui/sprites/icon"
)

type Asset1x21y struct {
	VersionPath string
}

func NewAsset1x21y(version string) *Asset1x21y {
	return &Asset1x21y{
		VersionPath: global.VersionPath(version),
	}
}

func (a *Asset1x21y) GetTexture(minecraftPath string) *Texture {
	if minecraftPath == "" || !strings.HasPrefix(minecraftPath, MinecraftPath) {
		return nil
	}
	return format(a.VersionPath, Root, "/", minecraftPath[len(MinecraftPath):], ImageSuffix)
}

func (a *Asset1x21y) GetPing(ping int) *Texture {
	if ping < 0 {
		return format(a.VersionPath, Icon, "/ping_unknown", ImageSuffix)
	} else if ping < 150 {
		return format(a.VersionPath, Icon, "/ping_5", ImageSuffix)
	} else if ping < 300 {
		return format(a.VersionPath, Icon, "/ping_4", ImageSuffix)
	} else if ping < 600 {
		return format(a.VersionPath, Icon, "/ping_3", ImageSuffix)
	} else if ping < 1000 {
		return format(a.VersionPath, Icon, "/ping_2", ImageSuffix)
	} else {
		return format(a.VersionPath, Icon, "/ping_1", ImageSuffix)
	}
}
