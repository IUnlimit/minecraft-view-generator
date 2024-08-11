package draw

import (
	"github.com/fogleman/gg"
	log "github.com/sirupsen/logrus"
	"testing"
)

func TestDrawInventory(t *testing.T) {
	inventory, err := Inventory("inventory.png", "skin.png", 2)
	if err != nil {
		log.Fatal(err)
	}
	_ = gg.SavePNG("./output/cover.png", inventory)
}
