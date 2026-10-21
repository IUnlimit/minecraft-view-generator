package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
)

func TestValidateImageOptionsAppliesTheMagnification(t *testing.T) {
	cfg := config.Default()
	cfg.MaxImageWidth = 300
	cfg.MaxImageHeight = 300
	cases := []struct {
		name    string
		options model.Options
		wantErr bool
	}{
		{"natural size", model.Options{}, false},
		{"explicit size", model.Options{Width: 200, Height: 100}, false},
		{"magnified size", model.Options{Width: 150, Scale: model.Scale{Ratio: 2}}, false},
		{"magnified past the limit", model.Options{Width: 200, Scale: model.Scale{Ratio: 2}}, true},
		{"target size", model.Options{Scale: model.Scale{Width: 300, Height: 200}}, false},
		{"target past the limit", model.Options{Scale: model.Scale{Width: 400}}, true},
		{"too small", model.Options{Width: 8}, true},
	}
	for _, testCase := range cases {
		err := validateImageOptions(testCase.options, cfg)
		if testCase.wantErr && err == nil {
			t.Fatalf("%s: expected an error", testCase.name)
		}
		if !testCase.wantErr && err != nil {
			t.Fatalf("%s: unexpected error %v", testCase.name, err)
		}
	}
}

// The response reports the size of the delivered image, magnification included.
func TestRenderReportsMagnifiedSize(t *testing.T) {
	server := testServer(t)
	cases := []struct {
		name    string
		options string
		width   int
		height  int
	}{
		{"fixed ratio", `{"width":40,"scale":2}`, 80, 80},
		{"fractional ratio", `{"scale":1.5}`, 192, 192},
		{"target width", `{"scale":{"width":300}}`, 300, 300},
		{"target box", `{"scale":{"width":1024,"height":240}}`, 240, 240},
	}
	for _, testCase := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"base64","options":`+testCase.options+`,"data":{"id":"minecraft:diamond"}}`))
		request.Header.Set("Content-Type", "application/json")
		record := httptest.NewRecorder()
		server.Handler().ServeHTTP(record, request)
		if record.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", testCase.name, record.Code, record.Body.String())
		}
		var value struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
		if err := json.Unmarshal(record.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.Width != testCase.width || value.Height != testCase.height {
			t.Fatalf("%s: size = %dx%d, want %dx%d", testCase.name, value.Width, value.Height, testCase.width, testCase.height)
		}
	}
}

// A single explicit dimension follows the natural canvas of the render type.
func TestRenderKeepsAspectRatioForOneDimension(t *testing.T) {
	server := testServer(t)
	body := `{"version":"1.21.11","type":"achievement","output":"base64","options":{"width":240},"data":{"title":"Stone Age","icon":{"id":"minecraft:stone"}}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(body))
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", record.Code, record.Body.String())
	}
	var value struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.Unmarshal(record.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Width != 240 || value.Height != 48 {
		t.Fatalf("size = %dx%d, want 240x48", value.Width, value.Height)
	}
}
