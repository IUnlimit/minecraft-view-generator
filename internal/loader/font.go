package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	"github.com/IUnlimit/minecraft-view-generator/pkg/url"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	log "github.com/sirupsen/logrus"
)

func LoadFont() {
	fontUrl := url.DefaultFont.Format()
	fontPath := global.FontsPath
	err := tools.DownloadFile(fontPath, fontUrl, false, true)
	if err != nil {
		log.Errorf("Failed to download default font from(%s), %v", fontUrl, err)
		return
	}
	if draw.Font == nil {
		err = draw.LoadFont(fontPath)
		if err != nil {
			log.Errorf("Failed to load font, %v", err)
		}
	}
	log.Infof("Font load success")
}
