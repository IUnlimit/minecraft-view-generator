package texture

import (
	"fmt"
	"image"
	"strings"
)

const (
	MinecraftPath = "minecraft:"
	Root          = "/assets/minecraft/textures"
	ImageSuffix   = ".png"
)

var assetMap = map[string]AssetsManager{
	"1.21":   NewAsset1x21y("1.21"),
	"1.21.1": NewAsset1x21y("1.21.1"),
}

type AssetsManager interface {
	// GetTexture param like minecraft:block/oak_planks
	GetTexture(minecraftPath string) (image.Image, error)
	// GetPlayerInventory with minecraft ICDA(first) and asset(second), 354 x 354
	GetPlayerInventory() (image.Image, error)
	// GetPing ping < 0 is considered disconnect
	GetPing(ping int) (image.Image, error)
}

func GetAssetManager(version string) (AssetsManager, error) {
	if assets, found := assetMap[version]; found {
		return assets, nil
	}
	return nil, fmt.Errorf("no asset with registered version(%s) found", version)
}

func format(args ...string) *Texture {
	var builder strings.Builder
	for _, arg := range args {
		builder.WriteString(arg)
	}
	return NewTexture(builder.String())
}
