package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/renderer"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// debugServer returns a server whose debug mode writes the request log into the returned buffer.
func debugServer(t *testing.T, debug bool) (*Server, *bytes.Buffer) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.RequireResources = false
	cfg.AllowFallbackRenderer = true
	cfg.Debug = debug
	manager, err := resources.NewManager(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: level}))
	return NewServer(cfg, manager, renderer.NewCompositor(renderer.PrimitiveAdapter{}), nil, logger), logs
}

func TestDebugLogPrintsReceivedPayload(t *testing.T) {
	server, logs := debugServer(t, true)
	payload := `{"version":"1.21.11","type":"item","output":"base64","data":{"id":"minecraft:diamond"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", record.Code, record.Body.String())
	}
	logged := logs.String()
	if !strings.Contains(logged, "msg=request") || !strings.Contains(logged, "status=200") {
		t.Fatalf("debug log does not describe the request: %s", logged)
	}
	// The payload is logged verbatim, so the operator sees exactly what the client sent.
	if !strings.Contains(logged, strings.ReplaceAll(payload, `"`, `\"`)) {
		t.Fatalf("debug log does not contain the request payload: %s", logged)
	}
	if !strings.Contains(logged, "bodyBytes=") || !strings.Contains(logged, "bodyTruncated=false") {
		t.Fatalf("debug log does not report the body size: %s", logged)
	}
}

func TestDebugLogIsSilentWithoutDebugMode(t *testing.T) {
	server, logs := debugServer(t, false)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/render", strings.NewReader(`{"version":"1.21.11","type":"item","output":"base64","data":{"id":"minecraft:diamond"}}`))
	record := httptest.NewRecorder()
	server.Handler().ServeHTTP(record, request)
	if record.Code != http.StatusOK {
		t.Fatalf("status = %d", record.Code)
	}
	if logs.Len() != 0 {
		t.Fatalf("request payload was logged without debug mode: %s", logs.String())
	}
}
