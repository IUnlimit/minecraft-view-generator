package model

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The parameter table of docs/api.md is the single place a consumer looks up what a render request
// accepts. These markers delimit it; the coverage tests below read the section between them.
const (
	paramsSectionStart = "<!-- params:start -->"
	paramsSectionEnd   = "<!-- params:end -->"
)

// TestParameterTableCoversEveryField is the mechanical guard against the wire contract drifting away
// from its documentation: every json field a render request decodes has to be named in the parameter
// table, wrapped in backticks. Adding a field without documenting it fails the build.
func TestParameterTableCoversEveryField(t *testing.T) {
	table := parameterTable(t)
	structs := []any{
		Request{}, Options{}, Item{}, Slot{}, ContainerData{}, TooltipData{}, SkinData{},
		PlayerInventoryData{}, PlayerListEntry{}, PlayerListData{}, MapMarker{}, MapData{},
		AchievementData{},
	}
	for _, value := range structs {
		kind := reflect.TypeOf(value)
		for index := 0; index < kind.NumField(); index++ {
			field := kind.Field(index)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			if !strings.Contains(table, "`"+name+"`") {
				t.Errorf("参数总表缺少 `%s`（%s.%s）", name, kind.Name(), field.Name)
			}
		}
	}
}

// TestParameterTableCoversEveryType keeps the type table complete, so a caller can always find which
// data structure a type decodes.
func TestParameterTableCoversEveryType(t *testing.T) {
	table := parameterTable(t)
	for _, kind := range SupportedTypes() {
		if !strings.Contains(table, "`"+kind+"`") {
			t.Errorf("参数总表缺少 `%s`", kind)
		}
	}
}

func parameterTable(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "api.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(raw)
	start := strings.Index(text, paramsSectionStart)
	if start < 0 {
		t.Fatalf("%s has no %s marker", path, paramsSectionStart)
	}
	end := strings.Index(text, paramsSectionEnd)
	if end < start {
		t.Fatalf("%s has no %s marker after the start marker", path, paramsSectionEnd)
	}
	return text[start+len(paramsSectionStart) : end]
}
