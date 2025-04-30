package graph

import (
	"os"
	"testing"
)

func TestDo(t *testing.T) {
	err := os.Chdir("/home/illtamer/Code/go/goland/minecraft-view-generator")
	if err != nil {
		t.Fatal(err)
	}
	err = os.Setenv("SDL_VIDEODRIVER", "offscreen")
	if err != nil {
		t.Fatal(err)
	}
	err = Do()
	if err != nil {
		t.Fatal(err)
	}
}
