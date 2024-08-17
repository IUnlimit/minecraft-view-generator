package lang

import (
	"fmt"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/buger/jsonparser"
	"github.com/emirpasic/gods/maps/hashmap"
	"os"
)

const AssetsLangPath = "/assets/minecraft/lang"

// version(string): lang.json([]byte)
var m = hashmap.New()

func Load(version string, lang string) error {
	langPath := global.VersionPath(version) + AssetsLangPath + "/" + lang + ".json"
	if !tools.FileExists(langPath) {
		return fmt.Errorf("can't find lang(%s) in path(%s)", lang, langPath)
	}

	langBytes, err := os.ReadFile(langPath)
	if err != nil {
		return err
	}

	m.Put(version, langBytes)
	return nil
}

// Get content from {lang}.json
func Get(version string, path ...string) (string, error) {
	langBytes, found := m.Get(version)
	if !found {
		return "", fmt.Errorf("not found version(%s) cache", version)
	}

	return jsonparser.GetString(langBytes.([]byte), path...)
}
