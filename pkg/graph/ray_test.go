package graph

import (
	"os"
	"testing"
)

func TestDo(t *testing.T) {
	const modelPath = "./pkg/graph/model_parse.json"
	const savePath = "./pkg/graph/rendered_block.png"
	err := os.Chdir("/home/illtamer/Code/go/goland/minecraft-view-generator")
	if err != nil {
		t.Fatal(err)
	}
	err = os.Setenv("SDL_VIDEODRIVER", "offscreen")
	if err != nil {
		t.Fatal(err)
	}
	err = Do(modelPath, savePath)
	if err != nil {
		t.Fatal(err)
	}
}
