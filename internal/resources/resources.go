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
)

type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

type VersionState struct {
	Version       string    `json:"version"`
	Status        Status    `json:"status"`
	Stage         string    `json:"stage,omitempty"`
	Progress      float64   `json:"progress"`
	OfficialID    string    `json:"officialId,omitempty"`
	AssetIndex    string    `json:"assetIndex,omitempty"`
	OfficialHash  string    `json:"officialHash,omitempty"`
	AddonHash     string    `json:"addonHash,omitempty"`
	ResourceRoot  string    `json:"resourceRoot,omitempty"`
	ResourcePacks []string  `json:"resourcePacks,omitempty"`
	Warnings      []string  `json:"warnings,omitempty"`
	Error         string    `json:"error,omitempty"`
	StartedAt     time.Time `json:"startedAt,omitempty"`
	FinishedAt    time.Time `json:"finishedAt,omitempty"`
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
	Warnings     []string
}

type Manager struct {
	cfg    config.Config
	client *http.Client

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
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create resource directory %s: %w", dir, err)
		}
	}
	m := &Manager{cfg: cfg, client: client, states: make(map[string]*VersionState), jobs: make(map[string]struct{})}
	if err := m.loadStates(); err != nil {
		return nil, err
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
		if state.Status == StatusReady {
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
	var packRoots []string
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
	state("custom-resource-packs", 0.88)
	packRoots, err = m.prepareCustomPacks(version, filepath.Join(staging, "packs"))
	if err != nil {
		m.fail(version, fmt.Errorf("prepare custom resource packs: %w", err))
		_ = os.RemoveAll(staging)
		return
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
		current.ResourcePacks = make([]string, len(packRoots))
		for index, packRoot := range packRoots {
			current.ResourcePacks[index] = filepath.Join(root, "packs", filepath.Base(packRoot))
		}
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
	return Context{Version: version, Root: state.ResourceRoot, OfficialRoot: filepath.Join(state.ResourceRoot, "vanilla"), AddonRoot: filepath.Join(state.ResourceRoot, "addon"), PackRoots: append([]string(nil), state.ResourcePacks...), Warnings: append([]string(nil), state.Warnings...)}, true
}

func cloneState(state *VersionState) *VersionState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.ResourcePacks = append([]string(nil), state.ResourcePacks...)
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

func (m *Manager) prepareCustomPacks(version, target string) ([]string, error) {
	root := strings.TrimSpace(m.cfg.ResourcePackRoot)
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	candidates := make([]string, 0)
	if !info.IsDir() {
		candidates = append(candidates, root)
	} else {
		candidateRoot := root
		if versionInfo, versionErr := os.Stat(filepath.Join(root, version)); versionErr == nil && versionInfo.IsDir() {
			candidateRoot = filepath.Join(root, version)
		}
		entries, readErr := os.ReadDir(candidateRoot)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			candidate := filepath.Join(candidateRoot, entry.Name())
			if entry.IsDir() || strings.EqualFold(filepath.Ext(entry.Name()), ".zip") {
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.Strings(candidates)
	if len(candidates) > 32 {
		return nil, fmt.Errorf("resource pack count %d exceeds limit 32", len(candidates))
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(candidates))
	for index, candidate := range candidates {
		packTarget := filepath.Join(target, fmt.Sprintf("pack-%03d", index))
		candidateInfo, statErr := os.Stat(candidate)
		if statErr != nil {
			return nil, statErr
		}
		if candidateInfo.IsDir() {
			err = copyPackDirectory(candidate, packTarget, m.cfg.MaxArchiveFiles, m.cfg.MaxArchiveBytes)
		} else {
			archive, openErr := zip.OpenReader(candidate)
			if openErr != nil {
				return nil, openErr
			}
			err = extractZipEntries(archive.File, packTarget, m.cfg.MaxArchiveFiles, m.cfg.MaxArchiveBytes, func(entry *zip.File) (io.ReadCloser, error) { return entry.Open() })
			closeErr := archive.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err == nil {
			err = validatePackMetadata(packTarget)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(candidate), err)
		}
		roots = append(roots, packTarget)
	}
	return roots, nil
}

func validatePackMetadata(root string) error {
	metadataPath := filepath.Join(root, "pack.mcmeta")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("pack.mcmeta is required: %w", err)
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
		return fmt.Errorf("invalid pack.mcmeta: %w", err)
	}
	if metadata.Pack.PackFormat != nil && (*metadata.Pack.PackFormat < 1 || *metadata.Pack.PackFormat > 100) {
		return fmt.Errorf("pack_format %d is incompatible with Minecraft 1.21.11", *metadata.Pack.PackFormat)
	}
	if metadata.Pack.MaxFormat != nil && (*metadata.Pack.MaxFormat < 1 || *metadata.Pack.MaxFormat > 100) {
		return fmt.Errorf("max_format %d is incompatible with Minecraft 1.21.11", *metadata.Pack.MaxFormat)
	}
	if len(metadata.Pack.MinFormat) > 0 {
		var min int
		if json.Unmarshal(metadata.Pack.MinFormat, &min) != nil {
			var tuple []int
			if err := json.Unmarshal(metadata.Pack.MinFormat, &tuple); err != nil || len(tuple) == 0 {
				return fmt.Errorf("min_format is incompatible with Minecraft 1.21.11")
			}
			min = tuple[0]
		}
		if min < 1 || min > 100 {
			return fmt.Errorf("min_format %d is incompatible with Minecraft 1.21.11", min)
		}
	}
	if supported := metadata.Pack.SupportedFormat; supported != nil {
		if supported.Min < 1 || supported.Max < supported.Min || supported.Min > 100 {
			return fmt.Errorf("supported_formats is incompatible with Minecraft 1.21.11")
		}
	}
	return nil
}

func copyPackDirectory(source, target string, maxFiles int, maxBytes int64) error {
	var files int
	var total int64
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o750)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("resource pack symlink %q is not allowed", relative)
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o750)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("resource pack entry %q is not a regular file", relative)
		}
		files++
		if files > maxFiles {
			return fmt.Errorf("resource pack contains more than %d files", maxFiles)
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if fileInfo.Size() > maxBytes || total > maxBytes-fileInfo.Size() {
			return fmt.Errorf("resource pack exceeds uncompressed size limit")
		}
		total += fileInfo.Size()
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeAtomic(destination, data, 0o640)
	})
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
