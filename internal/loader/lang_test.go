package loader

import (
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"github.com/IUnlimit/minecraft-view-generator/pkg/lang"
	"os"
	"testing"
)

func TestGet(t *testing.T) {
	err := os.Chdir("/home/illtamer/Code/go/goland/minecraft-view-generator")
	if err != nil {
		t.Fatal(err)
	}
	conf.Init()
	logger.Init()
	Init()

	version := global.LatestVersion
	err = lang.Load(version, "en_us")
	if err != nil {
		t.Fatal(err)
	}
	get, err := lang.Get(version, "container.enderchest")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(get)
}
