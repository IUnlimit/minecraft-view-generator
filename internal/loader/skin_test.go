package loader

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"os"
	"testing"
)

func TestLoadSkinByName(t *testing.T) {
	err := os.Chdir("/home/illtamer/Code/go/goland/minecraft-view-generator")
	if err != nil {
		t.Fatal(err)
	}
	conf.Init()
	logger.Init()
	Init()

	skin, err := LoadSkinByName("IllTamer", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(skin)
}
