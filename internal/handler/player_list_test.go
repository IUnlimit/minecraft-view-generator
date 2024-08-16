package handler

import (
	"os"
	"testing"

	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/loader"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
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
	request := &model.PlayerListRequest{
		Entry: []*model.PlayerListRequestEntry{
			{
				PlayerName: "[OP] IllTamer",
				Ping:       50,
			},
		},
	}
	_, err = GetPlayerList(request)
	if err != nil {
		t.Fatal(err)
	}
}
