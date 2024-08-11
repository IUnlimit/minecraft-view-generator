package handler

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/loader"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"os"
	"testing"
)

func TestGetPlayerList(t *testing.T) {
	err := os.Chdir("/home/illtamer/Code/go/goland/minecraft-view-generator")
	if err != nil {
		t.Fatal(err)
	}
	conf.Init()
	logger.Init()
	loader.Init()
	players := make([]any, 0)
	players = append(players, "§l§d[OP] §r§6IllTamer")
	err = GetPlayerList(players)
	if err != nil {
		t.Fatal(err)
	}
}
