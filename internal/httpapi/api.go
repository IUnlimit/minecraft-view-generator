package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/renderer"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

type Server struct {
	cfg        config.Config
	resources  *resources.Manager
	compositor *renderer.Compositor
	logger     *slog.Logger
	renderSem  chan struct{}
}

func NewServer(cfg config.Config, resourceManager *resources.Manager, compositor *renderer.Compositor, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, resources: resourceManager, compositor: compositor, logger: logger, renderSem: make(chan struct{}, cfg.MaxConcurrentRenders)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/versions", s.versions)
	mux.HandleFunc("PUT /api/v1/resources/{version}", s.prepare)
	mux.HandleFunc("GET /api/v1/resources/{version}", s.resourceState)
	mux.HandleFunc("POST /api/v1/render", s.render)
	mux.HandleFunc("GET /api/v1/openapi.json", s.openapi)
	mux.HandleFunc("GET /docs", s.docs)
	mux.HandleFunc("GET /scalar", s.docs)
	return s.withCommon(s.withAuth(mux))
}

func (s *Server) withCommon(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (contains(s.cfg.CORSAllowedOrigins, "*") || contains(s.cfg.CORSAllowedOrigins, origin)) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey == "" || r.URL.Path == "/healthz" || r.URL.Path == "/docs" || r.URL.Path == "/scalar" || r.URL.Path == "/api/v1/openapi.json" {
			next.ServeHTTP(w, r)
			return
		}
		provided := r.Header.Get("X-API-Key")
		if provided == "" {
			provided = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid API key is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	backend := renderer.Info(s.compositor.Adapter)
	status := "ok"
	if backend.Status != "ready" {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "renderer": backend.Name, "rendererVersion": backend.Version, "backend": backend, "time": time.Now().UTC()})
}

func (s *Server) versions(w http.ResponseWriter, r *http.Request) {
	values, err := s.resources.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource_list_failed", err.Error())
		return
	}
	publicValues := make([]map[string]any, 0, len(values))
	for _, value := range values {
		publicValues = append(publicValues, map[string]any{
			"version":    value.Version,
			"status":     value.Status,
			"progress":   value.Progress,
			"stage":      value.Stage,
			"officialId": value.OfficialID,
			"addonHash":  value.AddonHash,
			"error":      value.Error,
			"ready":      value.Status == resources.StatusReady,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": publicValues})
}

func (s *Server) prepare(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	state, started, err := s.resources.Prepare(version)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_version", err.Error())
		return
	}
	status := http.StatusAccepted
	if !started && state.Status == resources.StatusReady {
		status = http.StatusOK
	}
	writeJSON(w, status, publicResourceState(state))
}

func (s *Server) resourceState(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	state, ok := s.resources.State(version)
	if !ok {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource state does not exist")
		return
	}
	writeJSON(w, http.StatusOK, publicResourceState(state))
}

func publicResourceState(state resources.VersionState) map[string]any {
	return map[string]any{
		"version":       state.Version,
		"status":        state.Status,
		"stage":         state.Stage,
		"progress":      state.Progress,
		"officialId":    state.OfficialID,
		"assetIndex":    state.AssetIndex,
		"officialHash":  state.OfficialHash,
		"addonHash":     state.AddonHash,
		"resourcePacks": len(state.ResourcePacks),
		"warnings":      state.Warnings,
		"error":         state.Error,
		"startedAt":     state.StartedAt,
		"finishedAt":    state.FinishedAt,
	}
}

func (s *Server) render(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > s.cfg.MaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds configured limit")
		return
	}
	requestBody := http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	defer requestBody.Close()
	var request model.Request
	decoder := json.NewDecoder(requestBody)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds configured limit")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		}
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON value")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		}
		return
	}
	if err := request.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := validateImageOptions(request.Options, s.cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_image_options", err.Error())
		return
	}
	data, err := request.DecodeData()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_data", err.Error())
		return
	}
	resource, ready := s.resources.Context(request.Version)
	if !ready {
		if s.cfg.RequireResources {
			writeError(w, http.StatusConflict, "resources_not_ready", "prepare this Minecraft version before rendering")
			return
		}
		resource = resources.Context{Version: request.Version}
	}
	backend := renderer.Info(s.compositor.Adapter)
	if backend.Status != "ready" && !s.cfg.AllowFallbackRenderer {
		writeError(w, http.StatusServiceUnavailable, "renderer_unavailable", backend.Error)
		return
	}
	select {
	case s.renderSem <- struct{}{}:
		defer func() { <-s.renderSem }()
	case <-r.Context().Done():
		writeError(w, http.StatusRequestTimeout, "request_cancelled", r.Context().Err().Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.MaxRenderDuration)
	defer cancel()
	imageValue, err := s.compositor.Render(ctx, request, data, resource)
	if err != nil {
		s.logger.Error("render failed", "type", request.Type, "version", request.Version, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "render_failed", err.Error())
		return
	}
	if imageValue.Bounds().Dx() > s.cfg.MaxImageWidth || imageValue.Bounds().Dy() > s.cfg.MaxImageHeight {
		writeError(w, http.StatusUnprocessableEntity, "image_too_large", "rendered image exceeds configured limits")
		return
	}
	if request.Output == model.OutputBase64 {
		encoded, encodeErr := encodePNG(imageValue)
		if encodeErr != nil {
			writeError(w, http.StatusInternalServerError, "encode_failed", encodeErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"mimeType": "image/png", "encoding": "base64", "data": base64.StdEncoding.EncodeToString(encoded), "width": imageValue.Bounds().Dx(), "height": imageValue.Bounds().Dy()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", "inline; filename=render.png")
	w.WriteHeader(http.StatusOK)
	if err := png.Encode(w, imageValue); err != nil {
		s.logger.Error("write PNG failed", "error", err)
	}
}

func (s *Server) openapi(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, openAPIDocument)
}

func (s *Server) docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html>
<html lang="en">
<head>
  <title>Minecraft View Generator API</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</head>
<body>
  <div id="app"></div>
  <script type="module">
    import { createApiReference } from 'https://cdn.jsdelivr.net/npm/@scalar/api-reference/esm.js'

    createApiReference('#app', {
      url: '/api/v1/openapi.json',
      pageTitle: 'Minecraft View Generator API',
    })
  </script>
</body>
</html>`)
}

func validateImageOptions(options model.Options, cfg config.Config) error {
	if options.Width > cfg.MaxImageWidth || options.Height > cfg.MaxImageHeight {
		return fmt.Errorf("requested image dimensions exceed configured limits")
	}
	if options.Width != 0 && options.Width < 16 || options.Height != 0 && options.Height < 16 {
		return fmt.Errorf("requested image dimensions must be at least 16 pixels")
	}
	return nil
}

func encodePNG(value image.Image) ([]byte, error) {
	var buffer syncBuffer
	if err := png.Encode(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

type syncBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *syncBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, value...)
	return len(value), nil
}
func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
