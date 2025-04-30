package graph

import (
	"github.com/kylelemons/godebug/pretty"
	"testing"
)

func TestLoadModel(t *testing.T) {
	path := "/home/illtamer/Code/go/goland/minecraft-view-generator/config/assets/1.21.1/assets/minecraft/models/block/enchanting_table.json"
	model, err := LoadModel(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(pretty.Sprint(model))
}
