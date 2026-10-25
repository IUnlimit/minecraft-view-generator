package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/custom"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
	"github.com/illtamer/minecraft-view-generator2/internal/resources"
)

// withCustomArt resolves the drawing instruction of every custom item the request names. A key the
// index does not know is skipped, so the item keeps rendering as the vanilla item of its material;
// the catalog reports the reason once per index instead of once per request.
func (s *Server) withCustomArt(resource resources.Context, data any) resources.Context {
	if s.custom == nil {
		return resource
	}
	for _, item := range model.Items(data) {
		if item.CustomData == nil || strings.TrimSpace(item.ID) == "" {
			continue
		}
		key := model.CustomArtKey(item.ID, *item.CustomData)
		if _, resolved := resource.CustomArt[key]; resolved {
			continue
		}
		art, _, ok := s.custom.Art(item.ID, *item.CustomData)
		if !ok {
			continue
		}
		if resource.CustomArt == nil {
			resource.CustomArt = make(map[string]model.CustomArt, 1)
		}
		resource.CustomArt[key] = art
	}
	return resource
}

// fontSource reports where the glyphs come from. The Unifont archive is the one the Chinese labels
// need, and the overlay manifest records whether this tree actually received it.
type fontSource struct {
	Definition        string `json:"definition,omitempty"`
	DefinitionPresent bool   `json:"definitionPresent"`
	Unifont           string `json:"unifont,omitempty"`
	UnifontPresent    bool   `json:"unifontPresent"`
	Manifest          string `json:"manifest,omitempty"`
	ManifestPresent   bool   `json:"manifestPresent"`
}

// versionSource is one prepared Minecraft version with the paths a render reads.
type versionSource struct {
	Version      string     `json:"version"`
	Status       string     `json:"status"`
	ResourceRoot string     `json:"resourceRoot,omitempty"`
	VanillaRoot  string     `json:"vanillaRoot,omitempty"`
	AddonRoot    string     `json:"addonRoot,omitempty"`
	PackRoots    []string   `json:"packRoots,omitempty"`
	Font         fontSource `json:"font"`
	Warnings     []string   `json:"warnings,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// resourceSources is the body of GET /api/v1/resources/sources: every directory and file the service
// reads to draw a picture, so an operator can tell a missing pack from a missing index.
type resourceSources struct {
	DataDir string `json:"dataDir"`
	// ResourcePacks is the very report GET /api/v1/resources/packs returns. The pack stack is global,
	// so it is published once here instead of being repeated per version; the packRoots of each
	// version are the same ordered roots the renderer is handed.
	ResourcePacks  resources.PackReport `json:"resourcePacks"`
	Versions       []versionSource      `json:"versions"`
	CustomContents custom.Sources       `json:"customContents"`
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	versions, err := s.resources.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resource_list_failed", err.Error())
		return
	}
	report := resourceSources{
		DataDir:        s.cfg.DataDir,
		ResourcePacks:  s.resources.PackReport(),
		Versions:       make([]versionSource, 0, len(versions)),
		CustomContents: s.customSources(),
	}
	packRoots := s.resources.PackRoots()
	for _, info := range versions {
		state, ok := s.resources.State(info.Version)
		if !ok {
			continue
		}
		report.Versions = append(report.Versions, describeVersion(state, packRoots))
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) reloadCustom(w http.ResponseWriter, _ *http.Request) {
	if s.custom == nil {
		writeJSON(w, http.StatusOK, map[string]any{"customContents": custom.Sources{Status: custom.StatusPending}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"customContents": s.custom.Reload()})
}

func (s *Server) customSources() custom.Sources {
	if s.custom == nil {
		return custom.Sources{
			Root:       s.cfg.CustomContentsDir,
			ItemsCache: s.cfg.CustomItemsCache,
			Status:     custom.StatusPending,
		}
	}
	return s.custom.Sources()
}

// describeVersion expands one prepared version into the paths a render actually opens. packRoots is
// the live pack stack: it no longer belongs to the version tree, so an operator who reorders packs
// sees the new order here without preparing the version again.
func describeVersion(state resources.VersionState, packRoots []string) versionSource {
	report := versionSource{
		Version:      state.Version,
		Status:       string(state.Status),
		ResourceRoot: state.ResourceRoot,
		PackRoots:    append([]string(nil), packRoots...),
		Warnings:     append([]string(nil), state.Warnings...),
		Error:        state.Error,
	}
	if state.ResourceRoot == "" {
		return report
	}
	report.VanillaRoot = filepath.Join(state.ResourceRoot, "vanilla")
	report.AddonRoot = filepath.Join(state.ResourceRoot, "addon")
	report.Font = fontSource{
		Definition: filepath.Join(report.VanillaRoot, "assets", "minecraft", "font", "default.json"),
		Unifont:    filepath.Join(report.VanillaRoot, "assets", "minecraft", "font", "unifont.zip"),
		Manifest:   filepath.Join(state.ResourceRoot, "font-overlay.json"),
	}
	report.Font.DefinitionPresent = fileExists(report.Font.Definition)
	report.Font.UnifontPresent = fileExists(report.Font.Unifont)
	report.Font.ManifestPresent = fileExists(report.Font.Manifest)
	return report
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
