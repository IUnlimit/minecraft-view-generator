package texture

import (
	"errors"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/tools"
	log "github.com/sirupsen/logrus"
	"image"
	"strings"
)

const (
	Icon          = Root + "/gui/sprites/icon"
	Container     = Root + "/gui/container"
	ICDAContainer = Root + "/interactivechatdiscordsrvaddon/gui"
)

type Asset1x21y struct {
	VersionPath string
}

func NewAsset1x21y(version string) *Asset1x21y {
	return &Asset1x21y{
		VersionPath: global.VersionPath(version),
	}
}

func (a *Asset1x21y) GetTexture(minecraftPath string) (image.Image, error) {
	if minecraftPath == "" || !strings.HasPrefix(minecraftPath, MinecraftPath) {
		return nil, errors.New("unmatched minecraft texture format")
	}
	t := format(a.VersionPath, Root, "/", minecraftPath[len(MinecraftPath):], ImageSuffix)
	return t.Image, t.load()
}

func (a *Asset1x21y) GetPlayerInventory() (image.Image, error) {
	icdaT := format(a.VersionPath, ICDAContainer, "/player_inventory", ImageSuffix)
	if icdaT.load() != nil {
		t := format(a.VersionPath, Container, "/inventory", ImageSuffix)
		log.Warnf("There's no ICDA files found, use origin resource with path(%s)", t.Path)
		cut, err := tools.Cut(t.Image, 0, 0, 176, 166)
		if err != nil {
			return nil, err
		}
		resize := tools.Resize(cut, 176*2, 166*2)
		return resize, nil
	}
	icdaCut, err := tools.Cut(icdaT.Image, 2, 2, 354, 334)
	if err != nil {
		return nil, err
	}
	return icdaCut, nil
}

func (a *Asset1x21y) GetPing(ping int) (image.Image, error) {
	var t *Texture
	if ping < 0 {
		t = format(a.VersionPath, Icon, "/ping_unknown", ImageSuffix)
	} else if ping < 150 {
		t = format(a.VersionPath, Icon, "/ping_5", ImageSuffix)
	} else if ping < 300 {
		t = format(a.VersionPath, Icon, "/ping_4", ImageSuffix)
	} else if ping < 600 {
		t = format(a.VersionPath, Icon, "/ping_3", ImageSuffix)
	} else if ping < 1000 {
		t = format(a.VersionPath, Icon, "/ping_2", ImageSuffix)
	} else {
		t = format(a.VersionPath, Icon, "/ping_1", ImageSuffix)
	}
	return t.Image, t.load()
}
