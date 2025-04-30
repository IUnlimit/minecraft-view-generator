package graph

import (
	"encoding/json"
	"io"
	"os"
)

type Model struct {
	Parent   string            `json:"parent"`
	Textures map[string]string `json:"textures"`
	Elements []Element         `json:"elements"`
}

type Element struct {
	From  [3]float32      `json:"from"`
	To    [3]float32      `json:"to"`
	Faces map[string]Face `json:"faces"`
}

type Face struct {
	UV       [4]float32 `json:"uv"`
	Texture  string     `json:"texture"`
	Cullface string     `json:"cullface"`
}

func LoadModel(path string) (*Model, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var model Model
	err = json.Unmarshal(bytes, &model)
	if err != nil {
		return nil, err
	}

	return &model, nil
}
