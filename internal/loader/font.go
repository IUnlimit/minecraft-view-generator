package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	log "github.com/sirupsen/logrus"
)

const DefaultFont = "Minecraft.ttf"

// LoadFont loads font with specify name, if not exists, there will be a fatal
func LoadFont(fontName string) {
	if fontName == "" {
		fontName = DefaultFont
	}
	fontPath := global.FontsPath + "/" + fontName
	if !tools.FileExists(fontPath) {
		if fontName != DefaultFont {
			log.Fatalf("Can't find font %s in path(%s)", fontName, fontPath)
			return
		}

		fontUrl := url.DefaultFont.Format()
		err := tools.DownloadFile(fontPath, fontUrl, false, true)
		if err != nil {
			log.Errorf("Failed to download default font from(%s), %v", fontUrl, err)
			return
		}
	}

	if draw.Font != nil {
		log.Warn("The font instance already exists, the previously loaded font will be overwritten")
	}
	err := draw.LoadFont(fontPath)
	if err != nil {
		log.Errorf("Failed to load font, %v", err)
	}
	log.Infof("Font %s load success", fontName)
}
