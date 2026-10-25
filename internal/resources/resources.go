package resources

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
	"github.com/illtamer/minecraft-view-generator2/internal/model"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

type VersionState struct {
	Version      string    `json:"version"`
	Status       Status    `json:"status"`
	Stage        string    `json:"stage,omitempty"`
	Progress     float64   `json:"progress"`
	OfficialID   string    `json:"officialId,omitempty"`
	AssetIndex   string    `json:"assetIndex,omitempty"`
	OfficialHash string    `json:"officialHash,omitempty"`
	AddonHash    string    `json:"addonHash,omitempty"`
	ResourceRoot string    `json:"resourceRoot,omitempty"`
	Warnings     []string  `json:"warnings,omitempty"`
	Error        string    `json:"error,omitempty"`
	StartedAt    time.Time `json:"startedAt,omitempty"`
	FinishedAt   time.Time `json:"finishedAt,omitempty"`
}

type VersionInfo struct {
	Version      string  `json:"version"`
	Status       Status  `json:"status"`
	Progress     float64 `json:"progress"`
	Stage        string  `json:"stage,omitempty"`
	OfficialID   string  `json:"officialId,omitempty"`
	AddonHash    string  `json:"addonHash,omitempty"`
	ResourceRoot string  `json:"resourceRoot,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type Context struct {
	Version      string
	Root         string
	OfficialRoot string
	AddonRoot    string
	PackRoots    []string
	// PackRevision changes whenever the resource pack stack is reloaded or reordered. The fallback
	// renderer caches its font per context, and a pack replaced under the same name keeps its path,
	// so the roots alone cannot say whether the cached glyphs are still the ones on disk.
	PackRevision int64
	Warnings     []string
	// CustomArt holds the drawing instructions of the custom items one request references, keyed by
	// model.CustomArtKey. It is request scoped, so the same version can serve different user content
	// at the same time.
	CustomArt map[string]model.CustomArt
}

type Manager struct {
	cfg    config.Config
	client *http.Client
	packs  *packStore

	mu     sync.RWMutex
	states map[string]*VersionState
	jobs   map[string]struct{}
}

var versionSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func NewManager(cfg config.Config, client *http.Client) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	for _, dir := range []string{
		cfg.DataDir,
		filepath.Join(cfg.DataDir, "states"),
		filepath.Join(cfg.DataDir, "objects"),
		filepath.Join(cfg.DataDir, "resources"),
		cfg.PackDir(),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create resource directory %s: %w", dir, err)
		}
	}
	m := &Manager{cfg: cfg, client: client, packs: newPackStore(cfg), states: make(map[string]*VersionState), jobs: make(map[string]struct{})}
	if err := m.loadStates(); err != nil {
		return nil, err
	}
	// The pack stack is user content: an archive that cannot be read is reported and skipped, never
	// an error that keeps the service from starting.
	if _, err := m.packs.Reload(true); err != nil {
		slog.Warn("initial resource pack load failed", "directory", cfg.PackDir(), "error", err)
	}
	return m, nil
}

func (m *Manager) Config() config.Config { return m.cfg }

func (m *Manager) loadStates() error {
	entries, err := os.ReadDir(filepath.Join(m.cfg.DataDir, "states"))
	if err != nil {
		return fmt.Errorf("read resource states: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(m.cfg.DataDir, "states", entry.Name()))
		if readErr != nil {
			continue
		}
		var state VersionState
		if json.Unmarshal(data, &state) != nil || !versionSegment.MatchString(state.Version) {
			continue
		}
		if state.Status == StatusPending {
			state.Status = StatusFailed
			state.Error = "resource preparation was interrupted"
			state.FinishedAt = time.Now().UTC()
		}
		if state.Status == StatusReady && !fontAssetsPresent(state.ResourceRoot) {
			// A tree published before the font overlay existed answers 200 with every CJK glyph
			// drawn as the missing box, so it is restored here instead of being trusted. When even
			// the object cache is gone the version is no longer ready and prepare has to run again.
			if repairErr := m.restoreFontAssets(state.ResourceRoot); repairErr != nil {
				state.Status = StatusFailed
				state.Error = "font assets are missing from the resource tree: " + repairErr.Error() + "; prepare this version again"
				state.FinishedAt = time.Now().UTC()
				slog.Error("resource tree is missing its font assets", "version", state.Version, "root", state.ResourceRoot, "error", repairErr)
				m.persistState(&state)
			} else {
				slog.Warn("restored missing font assets", "version", state.Version, "root", state.ResourceRoot)
			}
		}
		m.states[state.Version] = cloneState(&state)
	}
	return nil
}

func (m *Manager) List(ctx context.Context) ([]VersionInfo, error) {
	// The official manifest is intentionally not fetched here. Listing must be
	// safe and fast even while upstream is unavailable; preparation is explicit.
	m.mu.RLock()
	result := make([]VersionInfo, 0, len(m.states))
	for _, state := range m.states {
		result = append(result, VersionInfo{Version: state.Version, Status: state.Status, Progress: state.Progress, Stage: state.Stage, OfficialID: state.OfficialID, AddonHash: state.AddonHash, ResourceRoot: state.ResourceRoot, Error: state.Error})
	}
	m.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func (m *Manager) State(version string) (VersionState, bool) {
	if !versionSegment.MatchString(version) {
		return VersionState{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.states[version]
	if !ok {
		return VersionState{}, false
	}
	return *cloneState(state), true
}

func (m *Manager) Prepare(version string) (VersionState, bool, error) {
	if !versionSegment.MatchString(version) {
		return VersionState{}, false, fmt.Errorf("invalid Minecraft version")
	}
	m.mu.Lock()
	if state, ok := m.states[version]; ok {
		// A tree prepared before the font overlay existed has every texture but no Unifont, which
		// silently degrades every label. Re-run instead of trusting the ready flag.
		if state.Status == StatusReady && fontAssetsPresent(state.ResourceRoot) {
			result := *cloneState(state)
			m.mu.Unlock()
			return result, false, nil
		}
		if _, running := m.jobs[version]; running {
			result := *cloneState(state)
			m.mu.Unlock()
			return result, false, nil
		}
	}
	state := &VersionState{Version: version, Status: StatusPending, Stage: "queued", Progress: 0, StartedAt: time.Now().UTC()}
	m.states[version] = state
	m.jobs[version] = struct{}{}
	result := *cloneState(state)
	m.mu.Unlock()
	m.persistState(state)
	go m.prepare(version)
	return result, true, nil
}

func (m *Manager) prepare(version string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	root := filepath.Join(m.cfg.DataDir, "resources", version)
	_ = os.RemoveAll(filepath.Join(m.cfg.DataDir, "resources", ".staging-"+version))
	staging := filepath.Join(m.cfg.DataDir, "resources", ".staging-"+version)
	if err := os.MkdirAll(staging, 0o750); err != nil {
		m.fail(version, err)
		return
	}
	state := func(stage string, progress float64) { m.update(version, stage, progress) }
	var warnings []string
	var err error
	var officialID, assetIndexHash, officialHash, addonHash string
	state("official-manifest", 0.02)
	var manifest VersionManifest
	if err = m.getJSON(ctx, m.cfg.MojangManifestURL, &manifest); err != nil {
		m.fail(version, fmt.Errorf("fetch Mojang version manifest: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	entry, ok := findVersion(manifest.Versions, version)
	if !ok {
		m.fail(version, fmt.Errorf("Minecraft version %q is not present in the official manifest", version))
		_ = os.RemoveAll(staging)
		return
	}
	officialID = entry.ID
	state("version-metadata", 0.05)
	var metadata VersionMetadata
	if err = m.getJSON(ctx, entry.URL, &metadata); err != nil {
		m.fail(version, fmt.Errorf("fetch Minecraft version metadata: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	assetIndexHash = metadata.AssetIndex.SHA1
	if assetIndexHash == "" {
		m.fail(version, errors.New("Minecraft version metadata has no asset index hash"))
		_ = os.RemoveAll(staging)
		return
	}
	state("asset-index", 0.08)
	assetIndexPath, err := m.materializeURL(ctx, metadata.AssetIndex.URL, metadata.AssetIndex.SHA1, metadata.AssetIndex.Size)
	if err != nil {
		m.fail(version, fmt.Errorf("download asset index: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	assetIndexData, err := os.ReadFile(assetIndexPath)
	if err != nil {
		m.fail(version, fmt.Errorf("read asset index: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	var assetIndex AssetIndex
	if err = json.Unmarshal(assetIndexData, &assetIndex); err != nil {
		m.fail(version, fmt.Errorf("decode asset index: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	if len(assetIndex.Objects) == 0 {
		warnings = append(warnings, "asset index contains no objects")
	}
	if err = writeAtomic(filepath.Join(staging, "official-assets-index.json"), assetIndexData, 0o640); err != nil {
		m.fail(version, fmt.Errorf("publish asset index to staging: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	state("asset-objects", 0.10)
	if err = m.downloadAssetObjects(ctx, assetIndex, func(done, total int) {
		progress := 0.10
		if total > 0 {
			progress += 0.45 * float64(done) / float64(total)
		}
		state("asset-objects", progress)
	}); err != nil {
		m.fail(version, fmt.Errorf("download asset objects: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	if metadata.Downloads.Client.URL != "" {
		state("client-jar", 0.58)
		clientPath, downloadErr := m.materializeURL(ctx, metadata.Downloads.Client.URL, metadata.Downloads.Client.SHA1, metadata.Downloads.Client.Size)
		if downloadErr != nil {
			m.fail(version, fmt.Errorf("download client jar: %w", downloadErr))
			_ = os.RemoveAll(staging)
			return
		}
		if err = extractAssetsFromJar(clientPath, filepath.Join(staging, "vanilla"), m.cfg.MaxArchiveFiles, m.cfg.MaxArchiveBytes); err != nil {
			m.fail(version, fmt.Errorf("extract client jar assets: %w", err))
			_ = os.RemoveAll(staging)
			return
		}
		officialHash = metadata.Downloads.Client.SHA1
	}
	// The client keeps the Unifont definitions and its 1.5 MB bitmap archive in the asset index
	// rather than in the jar, where only an empty placeholder ships. Overlay them so the resource
	// tree carries the font every label is drawn with.
	state("font-assets", 0.68)
	if err = materializeFontAssets(staging, assetIndex, m.cfg.DataDir); err != nil {
		m.fail(version, fmt.Errorf("materialize font assets: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	if m.cfg.AddonEnabled {
		state("addon-metadata", 0.72)
		addonMeta, addonWarnings, addonErr := m.prepareAddon(ctx, version, filepath.Join(staging, "addon"))
		warnings = append(warnings, addonWarnings...)
		if addonErr != nil {
			// Official resources remain usable when the optional addon endpoint is
			// unavailable. A warning is persisted and surfaced to callers.
			warnings = append(warnings, addonErr.Error())
		} else {
			addonHash = addonMeta.Hash
		}
	}
	state("publishing", 0.96)
	if err = publishDirectory(staging, root); err != nil {
		m.fail(version, fmt.Errorf("publish resource context: %w", err))
		_ = os.RemoveAll(staging)
		return
	}
	m.mu.Lock()
	if current, ok := m.states[version]; ok {
		current.Status = StatusReady
		current.Stage = "ready"
		current.Progress = 1
		current.OfficialID = officialID
		current.AssetIndex = assetIndexHash
		current.OfficialHash = officialHash
		current.AddonHash = addonHash
		current.ResourceRoot = root
		current.Warnings = append([]string(nil), warnings...)
		current.Error = ""
		current.FinishedAt = time.Now().UTC()
		m.persistStateLocked(current)
	}
	delete(m.jobs, version)
	m.mu.Unlock()
}

func (m *Manager) prepareAddon(ctx context.Context, version, target string) (AddonMetadata, []string, error) {
	var warnings []string
	var versions any
	if err := m.getJSON(ctx, m.cfg.AddonVersionsURL, &versions); err != nil {
		return AddonMetadata{}, warnings, fmt.Errorf("addon versions unavailable: %w", err)
	}
	if supported := addonSupportsVersion(versions, version); !supported {
		warnings = append(warnings, "addon has no explicit entry for this Minecraft version")
	}
	endpoint, err := withMinecraftVersion(m.cfg.AddonResourceURL, version)
	if err != nil {
		return AddonMetadata{}, warnings, fmt.Errorf("build addon metadata URL: %w", err)
	}
	var metadata AddonMetadata
	if err = m.getJSON(ctx, endpoint, &metadata); err != nil {
		return AddonMetadata{}, warnings, fmt.Errorf("fetch addon metadata: %w", err)
	}
	if err = os.MkdirAll(target, 0o750); err != nil {
		return AddonMetadata{}, warnings, err
	}
	metadataBytes, _ := json.MarshalIndent(metadata, "", "  ")
	if err = writeAtomic(filepath.Join(target, "metadata.json"), metadataBytes, 0o640); err != nil {
		return AddonMetadata{}, warnings, err
	}
	for index, entry := range metadata.Entries() {
		if entry.URL == "" {
			if entry.Hash != "" {
				warnings = append(warnings, fmt.Sprintf("addon entry %d has a hash but no URL", index))
			}
			continue
		}
		path, downloadErr := m.materializeURL(ctx, entry.URL, entry.Hash, entry.Size)
		if downloadErr != nil {
			return AddonMetadata{}, warnings, fmt.Errorf("download addon entry %d: %w", index, downloadErr)
		}
		name := entry.Path
		if name == "" {
			name = fmt.Sprintf("entry-%d.bin", index)
		}
		if err = copySafeFile(path, target, name); err != nil {
			return AddonMetadata{}, warnings, fmt.Errorf("store addon entry %d: %w", index, err)
		}
	}
	return metadata, warnings, nil
}

func (m *Manager) downloadAssetObjects(ctx context.Context, index AssetIndex, progress func(done, total int)) error {
	type object struct {
		name  string
		value AssetObject
	}
	objects := make([]object, 0, len(index.Objects))
	for name, value := range index.Objects {
		objects = append(objects, object{name: name, value: value})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].name < objects[j].name })
	if len(objects) > m.cfg.MaxArchiveFiles {
		return fmt.Errorf("asset index contains %d objects, limit is %d", len(objects), m.cfg.MaxArchiveFiles)
	}
	jobs := make(chan object)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	var done int
	worker := func() {
		defer wg.Done()
		for item := range jobs {
			_, err := m.materializeURL(ctx, assetURL(item.value.Hash), item.value.Hash, item.value.Size)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", item.name, err)
				}
				errMu.Unlock()
				continue
			}
			errMu.Lock()
			done++
			current := done
			errMu.Unlock()
			progress(current, len(objects))
		}
	}
	workerCount := 8
	if len(objects) < workerCount {
		workerCount = len(objects)
	}
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}
	for _, item := range objects {
		select {
		case jobs <- item:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return firstErr
}

func (m *Manager) materializeURL(ctx context.Context, rawURL, expectedHash string, expectedSize int64) (string, error) {
	if rawURL == "" {
		return "", errors.New("empty resource URL")
	}
	if err := m.validateURL(rawURL); err != nil {
		return "", err
	}
	if expectedHash != "" && !isSHA1(expectedHash) {
		return "", fmt.Errorf("invalid expected SHA-1 hash")
	}
	if expectedHash != "" {
		path := filepath.Join(m.cfg.DataDir, "objects", strings.ToLower(expectedHash))
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && (expectedSize <= 0 || info.Size() == expectedSize) {
			return path, nil
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "minecraft-view-generator/1.0")
	response, err := m.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > m.cfg.MaxDownloadBytes {
		return "", fmt.Errorf("resource exceeds download limit")
	}
	tmp, err := os.CreateTemp(filepath.Join(m.cfg.DataDir, "objects"), ".download-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hash := sha1.New()
	limited := io.LimitReader(response.Body, m.cfg.MaxDownloadBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), limited)
	if closeErr := tmp.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return "", copyErr
	}
	if written > m.cfg.MaxDownloadBytes {
		return "", fmt.Errorf("resource exceeds download limit")
	}
	if expectedSize > 0 && written != expectedSize {
		return "", fmt.Errorf("resource size %d does not match expected %d", written, expectedSize)
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if expectedHash != "" && !strings.EqualFold(actualHash, expectedHash) {
		return "", fmt.Errorf("resource SHA-1 %s does not match expected %s", actualHash, expectedHash)
	}
	objectPath := filepath.Join(m.cfg.DataDir, "objects", strings.ToLower(actualHash))
	if err = os.Chmod(tmpName, 0o640); err != nil {
		return "", err
	}
	if err = os.Rename(tmpName, objectPath); err != nil {
		if _, statErr := os.Stat(objectPath); statErr != nil {
			return "", err
		}
	}
	return objectPath, nil
}

func (m *Manager) getJSON(ctx context.Context, rawURL string, target any) error {
	if err := m.validateURL(rawURL); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "minecraft-view-generator/1.0")
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func (m *Manager) validateURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return fmt.Errorf("invalid resource URL")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("resource URL cannot contain credentials or fragments")
	}
	allowed := false
	for _, host := range m.cfg.AllowedResourceHosts {
		if strings.EqualFold(parsed.Hostname(), host) {
			allowed = true
			break
		}
	}
	for _, endpoint := range []string{m.cfg.MojangManifestURL, m.cfg.AddonVersionsURL, m.cfg.AddonResourceURL} {
		if configured, parseErr := url.Parse(endpoint); parseErr == nil && strings.EqualFold(configured.Hostname(), parsed.Hostname()) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("resource host %q is not allowlisted", parsed.Hostname())
	}
	if parsed.Scheme != "https" {
		// HTTP is useful for an explicitly configured local test mirror. It is
		// never accepted merely because a host appears in the default list.
		configuredHTTP := false
		for _, endpoint := range []string{m.cfg.MojangManifestURL, m.cfg.AddonVersionsURL, m.cfg.AddonResourceURL} {
			if configured, parseErr := url.Parse(endpoint); parseErr == nil && strings.EqualFold(configured.Hostname(), parsed.Hostname()) && configured.Scheme == parsed.Scheme {
				configuredHTTP = true
				break
			}
		}
		if !configuredHTTP {
			return fmt.Errorf("resource URL must use HTTPS")
		}
	}
	return nil
}

func (m *Manager) update(version, stage string, progress float64) {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.states[version]; ok {
		state.Stage = stage
		state.Progress = progress
		m.persistStateLocked(state)
	}
}

func (m *Manager) fail(version string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.states[version]; ok {
		state.Status = StatusFailed
		state.Stage = "failed"
		state.Error = err.Error()
		state.FinishedAt = time.Now().UTC()
		m.persistStateLocked(state)
	}
	delete(m.jobs, version)
}

func (m *Manager) persistState(state *VersionState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.persistStateLocked(state)
}

func (m *Manager) persistStateLocked(state *VersionState) {
	if state == nil {
		return
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err == nil {
		_ = writeAtomic(filepath.Join(m.cfg.DataDir, "states", state.Version+".json"), data, 0o640)
	}
}

func (m *Manager) Context(version string) (Context, bool) {
	state, ok := m.State(version)
	if !ok || state.Status != StatusReady || state.ResourceRoot == "" {
		return Context{}, false
	}
	// A ready flag on a tree that lost its font overlay would render every label as missing boxes;
	// refusing the context keeps that from being answered with a picture.
	if !fontAssetsPresent(state.ResourceRoot) {
		return Context{}, false
	}
	context := m.BaseContext(version)
	context.Root = state.ResourceRoot
	context.OfficialRoot = filepath.Join(state.ResourceRoot, "vanilla")
	context.AddonRoot = filepath.Join(state.ResourceRoot, "addon")
	context.Warnings = append([]string(nil), state.Warnings...)
	return context, true
}

// BaseContext is the part of the context that does not depend on a prepared version: the live user
// resource pack stack. It is what the no-resources fallback hands the renderer, so a pack still
// contributes its fonts when the vanilla tree is absent.
func (m *Manager) BaseContext(version string) Context {
	return Context{Version: version, PackRoots: m.packs.Roots(), PackRevision: m.packs.Revision()}
}

// PackRoots is the ordered pack stack a render is handed, independent of a prepared version.
func (m *Manager) PackRoots() []string { return m.packs.Roots() }

// PackReport answers the loaded resource packs and their priority.
func (m *Manager) PackReport() PackReport { return m.packs.Report() }

// SetPackOrder replaces the pack priority and returns the stack it produced.
func (m *Manager) SetPackOrder(order []string) (PackReport, error) { return m.packs.SetOrder(order) }

// WatchPacks reloads the stack whenever the pack directory changes. A zero interval disables it.
func (m *Manager) WatchPacks(ctx context.Context, interval time.Duration) {
	m.packs.Watch(ctx, interval)
}

func cloneState(state *VersionState) *VersionState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.Warnings = append([]string(nil), state.Warnings...)
	return &clone
}

func assetURL(hash string) string {
	if len(hash) < 2 {
		return ""
	}
	return "https://resources.download.minecraft.net/" + hash[:2] + "/" + hash
}

func isSHA1(value string) bool {
	if len(value) != sha1.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".atomic-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// fontAssetPrefix is the slice of the asset index the client loads as resources: the Unifont
// definition, its bitmap archive and the private use area variants. They live in the index because
// the jar only carries an empty placeholder for them.
const fontAssetPrefix = "minecraft/font/"

// fontOverlayManifest lists the font files the overlay wrote, relative to the vanilla tree. The
// client jar ships font/default.json and include/{default,space}.json but an empty placeholder for
// the Unifont ones, so without this record a tree published before the overlay existed looks like a
// valid font tree and every CJK glyph silently becomes the missing box.
const fontOverlayManifest = "font-overlay.json"

// materializeFontAssets copies the font entries of the asset index from the object cache into the
// resource tree, then records them in the overlay manifest. The index wins over the jar, so the
// empty placeholder the client jar ships is replaced by the real Unifont definition and the archive
// it names.
func materializeFontAssets(root string, index AssetIndex, dataDir string) error {
	names := make([]string, 0, len(index.Objects))
	for name := range index.Objects {
		if strings.HasPrefix(name, fontAssetPrefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	target := filepath.Join(root, "vanilla")
	written := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".zip") {
			continue
		}
		object := index.Objects[name]
		source := filepath.Join(dataDir, "objects", strings.ToLower(object.Hash))
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read asset object %s: %w", name, err)
		}
		relative := filepath.ToSlash(filepath.Join("assets", filepath.FromSlash(name)))
		destination := filepath.Join(target, filepath.FromSlash(relative))
		if !pathWithin(target, destination) {
			return fmt.Errorf("unsafe font asset path %q", name)
		}
		if err = writeAtomic(destination, data, 0o640); err != nil {
			return fmt.Errorf("publish font asset %s: %w", name, err)
		}
		written = append(written, relative)
	}
	if len(written) == 0 {
		return errors.New("the asset index carries no font assets")
	}
	manifest, err := json.MarshalIndent(struct {
		Files []string `json:"files"`
	}{Files: written}, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(root, fontOverlayManifest), append(manifest, '\n'), 0o640)
}

// fontAssetsPresent reports whether a prepared resource tree carries every font file the overlay
// wrote. It answers false for a tree published before the overlay existed (no manifest) and for one
// whose files were removed afterwards, which is how Prepare and loadStates notice they have to run
// the overlay again.
func fontAssetsPresent(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, fontOverlayManifest))
	if err != nil {
		return false
	}
	var manifest struct {
		Files []string `json:"files"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil || len(manifest.Files) == 0 {
		return false
	}
	for _, name := range manifest.Files {
		relative := filepath.FromSlash(name)
		if filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false
		}
		info, statErr := os.Stat(filepath.Join(root, "vanilla", relative))
		if statErr != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// restoreFontAssets puts the overlay back into a ready tree that was published without it — by an
// older binary, or before the overlay existed at all. The tree keeps the asset index it was
// prepared from, so this is a local copy out of the object cache and needs no network. It is the
// startup repair that turns a silently degraded tree back into a renderable one.
func (m *Manager) restoreFontAssets(root string) error {
	if fontAssetsPresent(root) {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root, "official-assets-index.json"))
	if err != nil {
		return fmt.Errorf("read the asset index of this tree: %w", err)
	}
	var index AssetIndex
	if err = json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("decode the asset index of this tree: %w", err)
	}
	if err = materializeFontAssets(root, index, m.cfg.DataDir); err != nil {
		return err
	}
	return nil
}

func publishDirectory(staging, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		backup := destination + ".old-" + fmt.Sprint(time.Now().UnixNano())
		if err = os.Rename(destination, backup); err != nil {
			return err
		}
		defer os.RemoveAll(backup)
	}
	return os.Rename(staging, destination)
}

func copySafeFile(source, root, name string) error {
	clean := filepath.Clean(name)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe addon path %q", name)
	}
	target := filepath.Join(root, clean)
	if !pathWithin(root, target) {
		return fmt.Errorf("unsafe addon path %q", name)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return writeAtomic(target, data, 0o640)
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// validatePackMetadata reads pack.mcmeta and answers the pack format it declares. A pack whose
// declared formats are outside the range this renderer understands is refused, which keeps an
// incompatible archive from silently replacing part of the vanilla tree.
func validatePackMetadata(root string) (int, error) {
	metadataPath := filepath.Join(root, "pack.mcmeta")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return 0, fmt.Errorf("pack.mcmeta is required: %w", err)
	}
	var metadata struct {
		Pack struct {
			PackFormat      *int            `json:"pack_format"`
			MaxFormat       *int            `json:"max_format"`
			MinFormat       json.RawMessage `json:"min_format"`
			SupportedFormat *struct {
				Min int `json:"min_inclusive"`
				Max int `json:"max_inclusive"`
			} `json:"supported_formats"`
		} `json:"pack"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return 0, fmt.Errorf("invalid pack.mcmeta: %w", err)
	}
	format := 0
	if metadata.Pack.PackFormat != nil {
		if *metadata.Pack.PackFormat < 1 || *metadata.Pack.PackFormat > 100 {
			return 0, fmt.Errorf("pack_format %d is incompatible with Minecraft 1.21.11", *metadata.Pack.PackFormat)
		}
		format = *metadata.Pack.PackFormat
	}
	if metadata.Pack.MaxFormat != nil {
		if *metadata.Pack.MaxFormat < 1 || *metadata.Pack.MaxFormat > 100 {
			return 0, fmt.Errorf("max_format %d is incompatible with Minecraft 1.21.11", *metadata.Pack.MaxFormat)
		}
		if format == 0 {
			format = *metadata.Pack.MaxFormat
		}
	}
	if len(metadata.Pack.MinFormat) > 0 {
		var min int
		if json.Unmarshal(metadata.Pack.MinFormat, &min) != nil {
			var tuple []int
			if err := json.Unmarshal(metadata.Pack.MinFormat, &tuple); err != nil || len(tuple) == 0 {
				return 0, fmt.Errorf("min_format is incompatible with Minecraft 1.21.11")
			}
			min = tuple[0]
		}
		if min < 1 || min > 100 {
			return 0, fmt.Errorf("min_format %d is incompatible with Minecraft 1.21.11", min)
		}
	}
	if supported := metadata.Pack.SupportedFormat; supported != nil {
		if supported.Min < 1 || supported.Max < supported.Min || supported.Min > 100 {
			return 0, fmt.Errorf("supported_formats is incompatible with Minecraft 1.21.11")
		}
		if format == 0 {
			format = supported.Max
		}
	}
	return format, nil
}

// Version manifest and asset metadata mirror Mojang's public JSON formats.
type VersionManifest struct {
	Versions []VersionEntry `json:"versions"`
}

type VersionEntry struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

type VersionMetadata struct {
	ID         string    `json:"id"`
	Assets     string    `json:"assets"`
	AssetIndex AssetRef  `json:"assetIndex"`
	Downloads  Downloads `json:"downloads"`
}

type AssetRef struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

type Downloads struct {
	Client AssetRef `json:"client"`
}

type AssetIndex struct {
	Objects map[string]AssetObject `json:"objects"`
}

type AssetObject struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

func findVersion(entries []VersionEntry, version string) (VersionEntry, bool) {
	for _, entry := range entries {
		if entry.ID == version {
			return entry, true
		}
	}
	return VersionEntry{}, false
}

type AddonMetadata struct {
	Hash              string            `json:"hash,omitempty"`
	ClientEntries     []AddonEntry      `json:"client-entries,omitempty"`
	AssetIndex        json.RawMessage   `json:"asset-index,omitempty"`
	DownloadedEntries []AddonEntry      `json:"downloaded-entries,omitempty"`
	RenameEntries     map[string]string `json:"rename-entries,omitempty"`
	ExtrasEntries     []AddonEntry      `json:"extras-entries,omitempty"`
}

type AddonEntry struct {
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
	Hash string `json:"hash,omitempty"`
	SHA1 string `json:"sha1,omitempty"`
	Size int64  `json:"size,omitempty"`
}

func (e AddonEntry) effectiveHash() string {
	if e.Hash != "" {
		return e.Hash
	}
	return e.SHA1
}

func (m AddonMetadata) Entries() []AddonEntry {
	entries := make([]AddonEntry, 0, len(m.ClientEntries)+len(m.DownloadedEntries)+len(m.ExtrasEntries))
	entries = append(entries, m.ClientEntries...)
	entries = append(entries, m.DownloadedEntries...)
	entries = append(entries, m.ExtrasEntries...)
	for i := range entries {
		entries[i].Hash = entries[i].effectiveHash()
	}
	return entries
}

func (m *AddonMetadata) UnmarshalJSON(data []byte) error {
	var raw struct {
		Hash              string            `json:"hash"`
		ClientEntries     json.RawMessage   `json:"client-entries"`
		AssetIndex        json.RawMessage   `json:"asset-index"`
		DownloadedEntries json.RawMessage   `json:"downloaded-entries"`
		RenameEntries     map[string]string `json:"rename-entries"`
		ExtrasEntries     json.RawMessage   `json:"extras-entries"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = AddonMetadata{Hash: raw.Hash, AssetIndex: raw.AssetIndex, RenameEntries: raw.RenameEntries}
	for name, payload := range map[string]json.RawMessage{
		"client-entries":     raw.ClientEntries,
		"downloaded-entries": raw.DownloadedEntries,
		"extras-entries":     raw.ExtrasEntries,
	} {
		if len(payload) == 0 || string(payload) == "null" {
			continue
		}
		entries, err := decodeAddonEntries(payload)
		if err != nil {
			return fmt.Errorf("decode addon %s: %w", name, err)
		}
		switch name {
		case "client-entries":
			m.ClientEntries = entries
		case "downloaded-entries":
			m.DownloadedEntries = entries
		case "extras-entries":
			m.ExtrasEntries = entries
		}
	}
	return nil
}

func decodeAddonEntries(payload json.RawMessage) ([]AddonEntry, error) {
	var entries []AddonEntry
	if err := json.Unmarshal(payload, &entries); err == nil {
		return entries, nil
	}
	var paths []string
	if err := json.Unmarshal(payload, &paths); err != nil {
		return nil, err
	}
	entries = make([]AddonEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, AddonEntry{Path: path})
	}
	return entries, nil
}

func addonSupportsVersion(value any, version string) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if addonSupportsVersion(item, version) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if key == version {
				return true
			}
			if addonSupportsVersion(item, version) {
				return true
			}
		}
	case string:
		return typed == version
	}
	return false
}

func withMinecraftVersion(rawURL, version string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("minecraftVersion", version)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func extractAssetsFromJar(path, target string, maxFiles int, maxBytes int64) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	assets := make([]*zip.File, 0, len(archive.File))
	for _, entry := range archive.File {
		name := filepath.ToSlash(entry.Name)
		if name == "assets" || strings.HasPrefix(name, "assets/") || name == "data" || strings.HasPrefix(name, "data/") {
			assets = append(assets, entry)
		}
	}
	return extractZipEntries(assets, target, maxFiles, maxBytes, func(entry *zip.File) (io.ReadCloser, error) { return entry.Open() })
}
