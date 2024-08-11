package component

import (
	"github.com/IUnlimit/minecraft-view-generator/pkg/draw"
	"github.com/fogleman/gg"
	"github.com/kylelemons/godebug/pretty"
	"testing"
)

func TestComponent_Parse(t *testing.T) {
	component := NewComponent("§l§a[A]§r§f我是§cIllTamer")
	suggestCtx := gg.NewContext(0, 0)
	err := draw.LoadFont("/home/illtamer/Code/go/goland/minecraft-view-generator/pkg/draw/Minecraft-AE(Chinese).ttf")
	if err != nil {
		t.Fatal(err)
	}
	face, err := draw.GetFontFace(24, 72)
	if err != nil {
		t.Fatal(err)
	}
	suggestCtx.SetFontFace(face)
	pretty.Print(component.Compute(suggestCtx, 3))
}
