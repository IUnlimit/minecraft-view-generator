package custom

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
)

// The catalog indexes the user maintained custom content: the packs of the ItemsAdder contents
// directory and the items_ids_cache.yml that maps a vanilla item to one of their entries. Both are
// edited by hand on a machine that is usually a network share, so the index is built from the
// configuration files only, thumbnails and models are resolved on demand, and a reload can rebuild
// everything without restarting the service.
//
// A pack this package cannot read is skipped with a warning: rendering vanilla items must never
// depend on the health of the user content.

// definition is one item a pack declares.
type definition struct {
	// ID is the namespaced item id, "fishing_pack:starfish".
	ID string
	// Pack is the directory name of the owning pack.
	Pack string
	// Root is the pack directory, which is also where its resources are looked up.
	Root string
	// Resource is the drawing instruction of the item, as far as the configuration declares it.
	Resource resource
}

type resource struct {
	Material   string
	Generate   bool
	Textures   []string
	ModelPath  string
	ModelPaths []string
}

// Catalog is the custom content index. It is safe for concurrent use: renders look items up while an
// operator may reload the index.
type Catalog struct {
	cfg    config.Config
	logger *slog.Logger

	mu    sync.RWMutex
	state *state
}

// state is one built index. A reload replaces it wholesale, so a render never sees half of an index.
type state struct {
	sources Sources
	cache   *Cache
	defs    map[string]definition
	roots   []string
	builtAt time.Time

	mu       sync.Mutex
	art      map[string]artEntry
	textures map[string]string
	layouts  map[string][]string
	reported map[string]struct{}
	resolved int
}

type artEntry struct {
	art    model.CustomArt
	reason string
}

// Sources is the report the resources endpoint publishes: which directories were read, which packs
// were picked up and how much of the index resolved.
type Sources struct {
	Root         string     `json:"root"`
	ItemsCache   string     `json:"itemsCache"`
	Status       string     `json:"status"`
	CachePresent bool       `json:"cachePresent"`
	ScannedAt    string     `json:"scannedAt,omitempty"`
	Packs        []PackInfo `json:"packs,omitempty"`
	Items        ItemCounts `json:"items"`
	Warnings     []string   `json:"warnings,omitempty"`
	Error        string     `json:"error,omitempty"`
}

type PackInfo struct {
	Name       string   `json:"name"`
	Root       string   `json:"root"`
	Namespaces []string `json:"namespaces,omitempty"`
	Items      int      `json:"items"`
	Configs    int      `json:"configs"`
}

type ItemCounts struct {
	Cached         int `json:"cached"`
	WithDefinition int `json:"withDefinition"`
	Resolved       int `json:"resolved"`
}

// Index status values.
const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusError   = "error"
)

const (
	maxConfigDepth = 4
	maxWarnings    = 64
)

// New creates the catalog and makes sure the contents directory exists, so an operator can drop a
// pack into it without creating the tree by hand.
func New(cfg config.Config, logger *slog.Logger) *Catalog {
	if logger == nil {
		logger = slog.Default()
	}
	catalog := &Catalog{cfg: cfg, logger: logger}
	catalog.state = &state{
		sources:  Sources{Root: cfg.CustomContentsDir, ItemsCache: cfg.CustomItemsCache, Status: StatusPending},
		cache:    &Cache{byKey: map[string]string{}},
		defs:     map[string]definition{},
		art:      map[string]artEntry{},
		textures: map[string]string{},
		layouts:  map[string][]string{},
		reported: map[string]struct{}{},
	}
	if cfg.CustomContentsDir == "" {
		return catalog
	}
	if err := os.MkdirAll(cfg.CustomContentsDir, 0o750); err != nil {
		catalog.logger.Warn("cannot create the custom contents directory", "path", cfg.CustomContentsDir, "error", err)
	}
	return catalog
}

// Ensure builds the index on first use. Renders call it so a request that arrives before the
// background warm up still finds its item, but only requests that actually name a custom item pay the
// scan.
func (c *Catalog) Ensure() {
	c.mu.RLock()
	built := c.state != nil && c.state.sources.Status != StatusPending
	c.mu.RUnlock()
	if built {
		return
	}
	c.Reload()
}

// Reload rebuilds the index and returns its report.
func (c *Catalog) Reload() Sources {
	c.mu.Lock()
	c.state = c.scan()
	c.mu.Unlock()
	sources := c.Sources()
	c.logger.Info("custom content index rebuilt",
		"root", sources.Root, "packs", len(sources.Packs), "items", sources.Items.Cached,
		"withDefinition", sources.Items.WithDefinition, "warnings", len(sources.Warnings))
	return sources
}

// Sources reports the index as it is, building it on first call.
func (c *Catalog) Sources() Sources {
	state := c.snapshot()
	state.mu.Lock()
	resolved := state.resolved
	state.mu.Unlock()
	sources := state.sources
	if !state.builtAt.IsZero() {
		sources.ScannedAt = state.builtAt.Format(time.RFC3339)
	}
	sources.Items.Resolved = resolved
	sources.Warnings = append([]string(nil), sources.Warnings...)
	return sources
}

// Art resolves the drawing instruction of a custom item. The material is the vanilla item the custom
// one is built on and customData is its ItemsAdder custom_model_data value. When the item is not
// indexed the returned reason says why, and the caller keeps rendering the vanilla item.
func (c *Catalog) Art(material string, customData int) (model.CustomArt, string, bool) {
	if customData < 0 {
		return model.CustomArt{}, "customData must not be negative", false
	}
	key := model.CustomArtKey(material, customData)
	state := c.snapshot()
	state.mu.Lock()
	if entry, ok := state.art[key]; ok {
		state.mu.Unlock()
		return entry.art, entry.reason, entry.art.Model != nil
	}
	state.mu.Unlock()

	art, reason, ok := c.resolve(state, key)
	state.mu.Lock()
	if _, known := state.art[key]; !known {
		state.art[key] = artEntry{art: art, reason: reason}
		if ok {
			state.resolved++
		}
	}
	state.mu.Unlock()
	if !ok && reason != "" {
		state.report(c.logger, key, reason)
	}
	return art, reason, ok
}

func (c *Catalog) snapshot() *state {
	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()
	if state != nil && state.sources.Status != StatusPending {
		return state
	}
	c.Reload()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// resolve turns an index key into a flat model plus its inline textures.
func (c *Catalog) resolve(state *state, key string) (model.CustomArt, string, bool) {
	id, ok := state.cache.LookupKey(key)
	if !ok {
		return model.CustomArt{}, "customData is not in items_ids_cache.yml for this material", false
	}
	entry, ok := state.defs[id]
	if !ok {
		return model.CustomArt{}, "the index names " + id + " but no pack configures it", false
	}
	return c.artFor(state, entry)
}

// scan builds one index from the file system.
func (c *Catalog) scan() *state {
	built := &state{
		cache:    &Cache{byKey: map[string]string{}},
		defs:     map[string]definition{},
		art:      map[string]artEntry{},
		textures: map[string]string{},
		layouts:  map[string][]string{},
		reported: map[string]struct{}{},
		builtAt:  time.Now(),
	}
	sources := Sources{Root: c.cfg.CustomContentsDir, ItemsCache: c.cfg.CustomItemsCache, Status: StatusReady}
	if c.cfg.CustomContentsDir == "" {
		sources.Status = StatusPending
		built.sources = sources
		return built
	}

	cache, warnings := c.loadCache()
	built.cache = cache
	sources.CachePresent = fileExists(c.cfg.CustomItemsCache)
	sources.Warnings = append(sources.Warnings, warnings...)

	entries, err := os.ReadDir(c.cfg.CustomContentsDir)
	if err != nil {
		sources.Status = StatusError
		sources.Error = err.Error()
		built.sources = sources
		return built
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		root := filepath.Join(c.cfg.CustomContentsDir, name)
		built.roots = append(built.roots, root)
		pack, packWarnings := c.scanPack(root, name, built)
		sources.Warnings = append(sources.Warnings, packWarnings...)
		if pack.Items > 0 || pack.Configs > 0 {
			sources.Packs = append(sources.Packs, pack)
		}
		if len(built.defs) >= c.cfg.MaxCustomItems && c.cfg.MaxCustomItems > 0 {
			sources.Warnings = append(sources.Warnings, "the index reached its configured item limit, later packs were not read")
			break
		}
	}
	withDefinition := 0
	for _, id := range built.cache.byKey {
		if _, ok := built.defs[id]; ok {
			withDefinition++
		}
	}
	sources.Items = ItemCounts{Cached: built.cache.Len(), WithDefinition: withDefinition}
	sources.Warnings = limit(sources.Warnings, maxWarnings)
	built.sources = sources
	return built
}

func (c *Catalog) loadCache() (*Cache, []string) {
	cache, warnings, err := LoadCache(c.cfg.CustomItemsCache)
	if err != nil {
		return &Cache{byKey: map[string]string{}}, []string{"cannot read " + c.cfg.CustomItemsCache + ": " + err.Error()}
	}
	if cache.Len() == 0 && !fileExists(c.cfg.CustomItemsCache) {
		warnings = append(warnings, "no items_ids_cache.yml at "+c.cfg.CustomItemsCache+", custom items are disabled")
	}
	return cache, warnings
}

// scanPack reads the configuration files of one pack. Only the configuration is read: models and
// textures are resolved on demand, because a pack can hold thousands of images.
func (c *Catalog) scanPack(root, name string, built *state) (PackInfo, []string) {
	pack := PackInfo{Name: name, Root: root}
	var warnings []string
	namespaces := map[string]struct{}{}
	for _, file := range configFiles(root) {
		pack.Configs++
		document, fileWarnings := readConfig(file)
		warnings = append(warnings, fileWarnings...)
		namespace := namespaceOf(document)
		if namespace == "" {
			warnings = append(warnings, filepath.Base(file)+" declares no info.namespace, skipping it")
			continue
		}
		items := yamlMapping(document["items"])
		for itemName, raw := range items {
			resourceMap := yamlMapping(yamlMapping(raw)["resource"])
			if resourceMap == nil {
				continue
			}
			id := namespace + ":" + itemName
			if _, duplicate := built.defs[id]; duplicate {
				warnings = append(warnings, "pack "+name+" redeclares "+id+", the later pack wins")
			}
			built.defs[id] = definition{ID: id, Pack: name, Root: root, Resource: resource{
				Material:   model.MaterialKey(yamlString(resourceMap["material"])),
				Generate:   yamlBool(resourceMap["generate"]),
				Textures:   yamlStrings(resourceMap["textures"]),
				ModelPath:  yamlString(resourceMap["model_path"]),
				ModelPaths: yamlStrings(resourceMap["model_paths"]),
			}}
			namespaces[namespace] = struct{}{}
			pack.Items++
		}
	}
	for namespace := range namespaces {
		pack.Namespaces = append(pack.Namespaces, namespace)
	}
	sort.Strings(pack.Namespaces)
	return pack, warnings
}

func readConfig(file string) (map[string]any, []string) {
	data, err := os.ReadFile(file)
	if err != nil {
		return map[string]any{}, []string{"cannot read " + filepath.Base(file) + ": " + err.Error()}
	}
	document, warnings := parseDocument(data)
	for index := range warnings {
		warnings[index] = filepath.Base(file) + ": " + warnings[index]
	}
	return document, warnings
}

// configFiles lists the YAML files of a pack. ItemsAdder keeps them under configs and items_packs;
// everything else in the pack is resources.
func configFiles(root string) []string {
	var files []string
	for _, top := range []string{"configs", "items_packs"} {
		base := filepath.Join(root, top)
		info, err := os.Stat(base)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := entry.Name()
			if entry.IsDir() {
				if strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				if strings.Count(strings.TrimPrefix(path, base), string(filepath.Separator)) >= maxConfigDepth {
					return filepath.SkipDir
				}
				return nil
			}
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
				return nil
			}
			files = append(files, path)
			return nil
		})
	}
	sort.Strings(files)
	return files
}

// namespaceOf reads the namespace a pack configures. ItemsAdder writes it as info.namespace; a few
// hand written files put it at the top level.
func namespaceOf(document map[string]any) string {
	if namespace := yamlString(yamlMapping(document["info"])["namespace"]); namespace != "" {
		return namespace
	}
	return yamlString(document["namespace"])
}

func limit(warnings []string, maximum int) []string {
	if len(warnings) <= maximum {
		return warnings
	}
	trimmed := append([]string(nil), warnings[:maximum]...)
	return append(trimmed, fmt.Sprintf("…and %d more warnings", len(warnings)-maximum))
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// report warns about a custom item once per index, so a request loop cannot flood the log.
func (s *state) report(logger *slog.Logger, key, reason string) {
	s.mu.Lock()
	_, seen := s.reported[key]
	if !seen {
		s.reported[key] = struct{}{}
	}
	s.mu.Unlock()
	if seen {
		return
	}
	logger.Warn("custom item is not drawn from the custom contents, rendering the vanilla item",
		"item", key, "reason", reason)
}
