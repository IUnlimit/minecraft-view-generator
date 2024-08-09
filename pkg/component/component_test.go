package component

import (
	"github.com/kylelemons/godebug/pretty"
	"testing"
)

func TestComponent_Parse(t *testing.T) {
	component := NewComponent("§l§a[A]§r§f我是§cIllTamer")
	chars := component.Parse()
	pretty.Print(chars)
}
