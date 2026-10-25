package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/custom"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/renderer"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

type Server struct {
	cfg        config.Config
	resources  *resources.Manager
	compositor *renderer.Compositor
	custom     *custom.Catalog
	logger     *slog.Logger
	renderSem  chan struct{}
}

// NewServer wires the HTTP layer. The custom content catalog may be nil, which is the same as an
// empty index: every request then renders the vanilla items it names.
func NewServer(cfg config.Config, resourceManager *resources.Manager, compositor *renderer.Compositor, catalog *custom.Catalog, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, resources: resourceManager, compositor: compositor, custom: catalog, logger: logger, renderSem: make(chan struct{}, cfg.MaxConcurrentRenders)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/versions", s.versions)
	mux.HandleFunc("PUT /api/v1/resources/{version}", s.prepare)
	mux.HandleFunc("GET /api/v1/resources/{version}", s.resourceState)
	mux.HandleFunc("GET /api/v1/resources/sources", s.sources)
	mux.HandleFunc("GET /api/v1/resources/packs", s.resourcePacks)
	mux.HandleFunc("PUT /api/v1/resources/packs", s.setResourcePackOrder)
	mux.HandleFunc("POST /api/v1/resources/custom/reload", s.reloadCustom)
	mux.HandleFunc("POST /api/v1/render", s.render)
	mux.HandleFunc("GET /api/v1/openapi.json", s.openapi)
	mux.HandleFunc("GET /docs", s.docs)
	mux.HandleFunc("GET /scalar", s.docs)
	return s.withDebugLog(s.withCommon(s.withAuth(mux)))
}

// debugBodyLimit bounds the part of a request body the debug log keeps: a render request may carry an
// inline skin PNG, and a single log line has to stay readable.
const debugBodyLimit = 64 << 10

// withDebugLog prints the received request payloads when debug mode is enabled. The body is copied
// while the handler reads it, so a request is never buffered or read twice.
func (s *Server) withDebugLog(next http.Handler) http.Handler {
	if !s.cfg.Debug {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		body := &debugBody{reader: r.Body, limit: debugBodyLimit}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = body
		}
		status := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(status, r)
		s.logger.Debug("request",
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"remote", r.RemoteAddr,
			"status", status.code(),
			"duration", time.Since(started).Round(time.Microsecond).String(),
			"contentLength", r.ContentLength,
			"bodyBytes", body.total,
			"bodyTruncated", body.truncated(),
			"payload", body.text(),
		)
	})
}

// debugBody copies a request body while it is being read, keeping at most limit bytes.
type debugBody struct {
	reader io.ReadCloser
	buffer bytes.Buffer
	limit  int
	total  int64
}

func (b *debugBody) Read(value []byte) (int, error) {
	read, err := b.reader.Read(value)
	if read > 0 {
		b.total += int64(read)
		if room := b.limit - b.buffer.Len(); room > 0 {
			b.buffer.Write(value[:min(read, room)])
		}
	}
	return read, err
}

func (b *debugBody) Close() error {
	return b.reader.Close()
}

func (b *debugBody) truncated() bool {
	return b.total > int64(b.buffer.Len())
}

func (b *debugBody) text() string {
	return b.buffer.String()
}

// statusRecorder remembers the status code a handler wrote without changing the response.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(value []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(value)
}

func (r *statusRecorder) code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
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
		// The version list is what the container health probe reads, so it stays reachable without a
		// key the same way the documentation routes are.
		if s.cfg.APIKey == "" || r.URL.Path == "/api/v1/versions" || r.URL.Path == "/docs" || r.URL.Path == "/scalar" || r.URL.Path == "/api/v1/openapi.json" {
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
	writeJSON(w, status, s.publicResourceState(state))
}

func (s *Server) resourceState(w http.ResponseWriter, r *http.Request) {
	version := r.PathValue("version")
	state, ok := s.resources.State(version)
	if !ok {
		writeError(w, http.StatusNotFound, "resource_not_found", "resource state does not exist")
		return
	}
	writeJSON(w, http.StatusOK, s.publicResourceState(state))
}

// resourcePacks reports the user resource pack stack: the directory that is read, every archive in
// load order and the unzipped tree each one contributes to a render.
func (s *Server) resourcePacks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.resources.PackReport())
}

// setResourcePackOrder replaces the load order. The body is the inverse of what resourcePacks
// publishes: every archive of the report, exactly once, in the new order. A later pack overrides an
// earlier one, so the last entry of the list is the one that wins.
func (s *Server) setResourcePackOrder(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > s.cfg.MaxBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds configured limit")
		return
	}
	body := http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
	defer body.Close()
	var request struct {
		Order []string `json:"order"`
	}
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pack_order", err.Error())
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_pack_order", "request body must contain exactly one JSON value")
		return
	}
	report, err := s.resources.SetPackOrder(request.Order)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_pack_order", err.Error())
		return
	}
	s.logger.Info("resource pack order updated", "order", request.Order)
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) publicResourceState(state resources.VersionState) map[string]any {
	loadedPacks := 0
	for _, pack := range s.resources.PackReport().Packs {
		if pack.Status == resources.PackLoaded {
			loadedPacks++
		}
	}
	return map[string]any{
		"version":       state.Version,
		"status":        state.Status,
		"stage":         state.Stage,
		"progress":      state.Progress,
		"officialId":    state.OfficialID,
		"assetIndex":    state.AssetIndex,
		"officialHash":  state.OfficialHash,
		"addonHash":     state.AddonHash,
		"resourcePacks": loadedPacks,
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
		resource = s.resources.BaseContext(request.Version)
	}
	resource = s.withCustomArt(resource, data)
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
		if errors.Is(err, renderer.ErrImageTooLarge) {
			writeError(w, http.StatusUnprocessableEntity, "image_too_large", err.Error())
			return
		}
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
	// A target size is checked as the final size of the picture; an explicit canvas is multiplied by
	// the fixed ratio so an impossible magnification is rejected before rendering starts. A fixed
	// ratio on a natural canvas is checked by the compositor, which only knows the canvas size after
	// the resource renderer has produced it.
	if targetWidth, targetHeight, ok := options.Scale.TargetSize(); ok {
		if targetWidth > cfg.MaxImageWidth || targetHeight > cfg.MaxImageHeight {
			return fmt.Errorf("requested output dimensions exceed configured limits")
		}
	}
	if ratio, ok := options.Scale.FixedRatio(); ok && ratio > 1 {
		if options.Width > 0 && int(math.Round(float64(options.Width)*ratio)) > cfg.MaxImageWidth {
			return fmt.Errorf("requested image width exceeds configured limits after the %gx magnification", ratio)
		}
		if options.Height > 0 && int(math.Round(float64(options.Height)*ratio)) > cfg.MaxImageHeight {
			return fmt.Errorf("requested image height exceeds configured limits after the %gx magnification", ratio)
		}
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
