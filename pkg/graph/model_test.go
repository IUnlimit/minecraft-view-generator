package graph

import (
	"github.com/kylelemons/godebug/pretty"
	"testing"
)

func TestLoadModel(t *testing.T) {
	path := "./model_parse.json"
	model, err := LoadModel(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(pretty.Sprint(model))
}
