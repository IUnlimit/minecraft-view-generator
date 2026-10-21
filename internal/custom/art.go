package custom

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
)

// A custom item is drawn from a model plus its textures, and both live inside the pack. The model is
// flattened here — its parent chain is walked until it reaches a vanilla model, which stays the
// parent so the renderer still gets the vanilla display transforms for the hand slots — and every
// texture of a custom namespace is inlined as a data URI. The renderer therefore never has to know
// where the packs are, and a reload cannot leave it pointing at a stale file.

// packLayouts are the directory shapes the packs in the wild use for their resource pack. ItemsAdder
// itself only writes the first, but packs assembled by hand ship assets/ or a bare textures/ tree,
// and icon_pack keeps its images directly under textures/.
var packLayouts = []string{
	"resourcepack/assets",
	"resource_pack/assets",
	"assets",
	"resourcepack",
	"resource_pack",
	"",
}

// maxParentDepth bounds a model parent chain, so a cycle cannot hang a render.
const maxParentDepth = 8

// artFor turns one indexed item into the drawing instruction the renderer consumes.
func (c *Catalog) artFor(state *state, entry definition) (model.CustomArt, string, bool) {
	namespace, _, _ := splitID(entry.ID)
	var document map[string]any
	var warnings []string
	if entry.Resource.Generate {
		document = generatedModel(namespace, entry.Resource.Textures)
	} else {
		path := entry.Resource.ModelPath
		if path == "" && len(entry.Resource.ModelPaths) > 0 {
			path = entry.Resource.ModelPaths[0]
		}
		if path == "" {
			_, name, _ := splitID(entry.ID)
			path = "item/" + name
		}
		loaded, ok := c.loadModelDocument(state, entry, joinID(namespace, path))
		if !ok {
			return model.CustomArt{}, "the model " + joinID(namespace, path) + " of " + entry.ID + " is missing", false
		}
		document = loaded
	}
	flat, chainWarnings := c.flattenModel(state, entry, document)
	warnings = append(warnings, chainWarnings...)
	textures, textureWarnings := c.inlineTextures(state, entry, flat)
	warnings = append(warnings, textureWarnings...)
	art := model.CustomArt{Model: flat, Textures: textures}
	for _, warning := range warnings {
		state.report(c.logger, entry.ID, warning)
	}
	if len(textures) == 0 && !hasVanillaTexture(flat) {
		return model.CustomArt{}, "the model of " + entry.ID + " references no texture that could be found", false
	}
	return art, "", true
}

// generatedModel builds the model ItemsAdder generates for an item whose textures are listed instead
// of modelled, which is how the flat icon packs declare their images.
func generatedModel(namespace string, textures []string) map[string]any {
	layers := map[string]any{}
	for index, texture := range textures {
		if index >= 32 {
			break
		}
		layers["layer"+strconv.Itoa(index)] = joinID(namespace, texture)
	}
	if len(layers) == 0 {
		layers["layer0"] = joinID(namespace, "item/unknown")
	}
	return map[string]any{"parent": "minecraft:item/generated", "textures": layers}
}

// flattenModel walks the parent chain of a custom model and merges it into one document. The walk
// stops at the first vanilla parent, which is kept as the parent of the result.
func (c *Catalog) flattenModel(state *state, entry definition, document map[string]any) (map[string]any, []string) {
	var warnings []string
	chain := []map[string]any{document}
	current := document
	parent := ""
	for depth := 0; depth < maxParentDepth; depth++ {
		parent = strings.TrimSpace(stringValue(current["parent"]))
		if parent == "" || strings.HasPrefix(parent, "builtin/") {
			break
		}
		namespace, _, ok := splitID(parent)
		if !ok || namespace == "minecraft" {
			break
		}
		next, found := c.loadModelDocument(state, entry, parent)
		if !found {
			warnings = append(warnings, "the parent model "+parent+" is missing, drawing what is left")
			parent = ""
			break
		}
		chain = append(chain, next)
		current = next
	}
	flat := map[string]any{}
	for index := len(chain) - 1; index >= 0; index-- {
		flat = mergeModel(flat, chain[index])
	}
	namespace, _, validParent := splitID(parent)
	switch {
	case !validParent:
		delete(flat, "parent")
	case strings.HasPrefix(parent, "builtin/"):
		flat["parent"] = "minecraft:item/generated"
	case namespace == "minecraft":
		flat["parent"] = parent
	default:
		// A cycle or a chain deeper than the limit: everything is already merged, so the result is
		// complete without a parent the renderer could not load anyway.
		delete(flat, "parent")
	}
	if _, ok := flat["textures"]; !ok {
		flat["textures"] = map[string]any{}
	}
	return flat, warnings
}

// mergeModel stacks a more specific model onto a more generic one, following the client's rule: a
// child replaces the scalars and the element list it declares, and merges its textures and display
// transforms key by key.
func mergeModel(base, child map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(child))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range child {
		switch key {
		case "textures", "display":
			existing, ok := merged[key].(map[string]any)
			if !ok {
				existing = map[string]any{}
			}
			incoming, ok := value.(map[string]any)
			if !ok {
				continue
			}
			combined := make(map[string]any, len(existing)+len(incoming))
			for name, entry := range existing {
				combined[name] = entry
			}
			for name, entry := range incoming {
				combined[name] = entry
			}
			merged[key] = combined
		case "elements":
			if list, ok := value.([]any); ok && len(list) > 0 {
				merged[key] = list
			}
		case "gui_light":
			if text := strings.TrimSpace(stringValue(value)); text != "" {
				merged[key] = text
			}
		default:
			merged[key] = value
		}
	}
	return merged
}

// inlineTextures reads every texture of a custom namespace and encodes it as a data URI.
func (c *Catalog) inlineTextures(state *state, entry definition, flat map[string]any) (map[string]string, []string) {
	var warnings []string
	textures := map[string]string{}
	declared, _ := flat["textures"].(map[string]any)
	for name, raw := range declared {
		id := normalizeTextureID(stringValue(raw))
		if id == "" {
			continue
		}
		namespace, _, _ := splitID(id)
		if namespace == "minecraft" {
			// Vanilla textures are read from the normalised resource tree, which already holds every
			// pack the service downloaded.
			declared[name] = id
			continue
		}
		if len(textures) >= c.cfg.MaxCustomLayers {
			warnings = append(warnings, "the item declares more than "+strconv.Itoa(c.cfg.MaxCustomLayers)+" custom textures, the rest is ignored")
			break
		}
		declared[name] = id
		encoded, ok, reason := c.inlineTexture(state, entry, id)
		if !ok {
			if reason != "" {
				warnings = append(warnings, reason)
			}
			continue
		}
		textures[id] = encoded
	}
	return textures, warnings
}

func (c *Catalog) inlineTexture(state *state, entry definition, id string) (string, bool, string) {
	state.mu.Lock()
	cached, ok := state.textures[id]
	state.mu.Unlock()
	if ok {
		return cached, cached != "", ""
	}
	namespace, path, _ := splitID(id)
	file, found := c.findFile(state, entry, namespace, "textures", path, ".png")
	if !found {
		state.mu.Lock()
		state.textures[id] = ""
		state.mu.Unlock()
		return "", false, "the texture " + id + " is not in any pack"
	}
	info, err := os.Stat(file)
	if err != nil || info.Size() > int64(c.cfg.MaxCustomTextureBytes) {
		state.mu.Lock()
		state.textures[id] = ""
		state.mu.Unlock()
		return "", false, "the texture " + id + " is larger than the configured maximum"
	}
	data, err := os.ReadFile(file)
	if err != nil {
		state.mu.Lock()
		state.textures[id] = ""
		state.mu.Unlock()
		return "", false, "cannot read the texture " + id
	}
	encoded := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	state.mu.Lock()
	state.textures[id] = encoded
	state.mu.Unlock()
	return encoded, true, ""
}

// loadModelDocument reads the JSON of a model by its resource location.
func (c *Catalog) loadModelDocument(state *state, entry definition, id string) (map[string]any, bool) {
	namespace, path, ok := splitID(id)
	if !ok {
		return nil, false
	}
	path = normalizeResourcePath(path, "models", ".json")
	file, found := c.findFile(state, entry, namespace, "models", path, ".json")
	if !found {
		return nil, false
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, false
	}
	return document, true
}

// findFile resolves a resource of one namespace to a file on disk. The pack that declares the item is
// searched first, then every other pack, because a pack may reuse the textures of another.
func (c *Catalog) findFile(state *state, entry definition, namespace, kind, path, extension string) (string, bool) {
	path = normalizeResourcePath(path, kind, extension)
	roots := make([]string, 0, len(state.roots)+1)
	roots = append(roots, entry.Root)
	for _, root := range state.roots {
		if root != entry.Root {
			roots = append(roots, root)
		}
	}
	for _, root := range roots {
		for _, layout := range c.layoutsOf(state, root) {
			base := root
			if layout != "" {
				base = filepath.Join(root, layout)
			}
			for _, candidate := range []string{
				filepath.Join(base, namespace, kind, path+extension),
				filepath.Join(base, kind, path+extension),
			} {
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate, true
				}
			}
		}
	}
	return "", false
}

// layoutsOf reports the layout directories a pack actually has. A pack on a network share answers a
// stat slowly, and a render can ask for dozens of textures, so the answer is cached per index and the
// searches only visit the shapes that exist.
func (c *Catalog) layoutsOf(state *state, root string) []string {
	state.mu.Lock()
	cached, ok := state.layouts[root]
	state.mu.Unlock()
	if ok {
		return cached
	}
	layouts := make([]string, 0, len(packLayouts))
	for _, layout := range packLayouts {
		base := root
		if layout != "" {
			base = filepath.Join(root, layout)
		}
		if info, err := os.Stat(base); err == nil && info.IsDir() {
			layouts = append(layouts, layout)
		}
	}
	state.mu.Lock()
	state.layouts[root] = layouts
	state.mu.Unlock()
	return layouts
}

// normalizeResourcePath strips the optional textures/ or models/ prefix and the extension packs write
// inconsistently, so one path form reaches the file system.
func normalizeResourcePath(path, kind, extension string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, kind+"/")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, extension)
	return path
}

// normalizeTextureID turns a model texture reference into the namespace:path form the renderer keys
// its textures by, following the client's rule that a reference without a namespace is vanilla.
func normalizeTextureID(value string) string {
	text := strings.TrimSpace(value)
	if text == "" || strings.HasPrefix(text, "#") {
		return ""
	}
	namespace, path, _ := splitID(text)
	path = normalizeResourcePath(path, "textures", ".png")
	if path == "" {
		return ""
	}
	return namespace + ":" + path
}

// hasVanillaTexture reports whether a model keeps at least one texture of the vanilla tree, which the
// renderer reads on its own.
func hasVanillaTexture(flat map[string]any) bool {
	declared, ok := flat["textures"].(map[string]any)
	if !ok {
		return false
	}
	for _, raw := range declared {
		if id := normalizeTextureID(stringValue(raw)); id != "" && strings.HasPrefix(id, "minecraft:") {
			return true
		}
	}
	return false
}

// splitID splits a resource location into its namespace and path, defaulting to minecraft.
func splitID(id string) (string, string, bool) {
	text := strings.TrimSpace(id)
	if text == "" {
		return "", "", false
	}
	if index := strings.IndexByte(text, ':'); index >= 0 {
		namespace := strings.TrimSpace(text[:index])
		path := text[index+1:]
		if namespace == "" {
			namespace = "minecraft"
		}
		return namespace, path, path != ""
	}
	return "minecraft", text, true
}

func joinID(namespace, path string) string {
	path = strings.TrimSpace(path)
	if namespace == "" || strings.HasPrefix(path, namespace+":") {
		return path
	}
	if strings.Contains(path, ":") {
		return path
	}
	return namespace + ":" + path
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
