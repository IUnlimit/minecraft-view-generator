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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/illtamer/minecraft-view-generator2/internal/config"
)

// The user resource packs are the last layer of the stack a render reads: the pack directory is
// scanned at startup, every archive is unzipped into a cache addressed by its content, and the
// ordered result is handed to the renderer as packRoots. Two things follow from that placement:
//
//   - The renderer already resolves a relative path by walking its roots backwards, so the LAST pack
//     of the list overrides every earlier one, which is the client's rule as well. Priority is
//     therefore just the position in this list.
//   - The list is owned here, not by a prepared version tree. Reordering or replacing a pack applies
//     to the next render, without preparing the version again and without a restart.

// Pack status values. A pack that cannot be read is listed as failed instead of blocking the stack:
// one broken archive must never take every render down with it.
const (
	PackLoaded = "loaded"
	PackFailed = "failed"
)

// Directory status values.
const (
	PackStatusReady = "ready"
	PackStatusError = "error"
)

const (
	// maxResourcePacks bounds the stack. Every render stats each root, and the renderer walks them
	// per texture lookup, so a directory of hundreds of archives is a configuration mistake.
	maxResourcePacks = 32
	// packManifestVersion is the schema of the order manifest.
	packManifestVersion = 1
	// packCacheDir is the derived unzip cache, next to the prepared version trees and dropped with
	// them when the data directory is rebuilt.
	packCacheDir = ".packs"
	// maxPackWarnings bounds the warnings one scan reports, so a directory full of stray files cannot
	// grow the report without limit.
	maxPackWarnings = 64
)

// Pack is one entry of the pack directory, as the report publishes it.
type Pack struct {
	File string `json:"file"`
	// Priority is the position in the load order, starting at one. A higher number is applied later
	// and therefore overrides the packs before it. It is meaningless for a failed pack.
	Priority   int       `json:"priority"`
	AddedAt    time.Time `json:"addedAt"`
	Root       string    `json:"root,omitempty"`
	Files      int       `json:"files,omitempty"`
	PackFormat int       `json:"packFormat,omitempty"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
}

// PackReport is the body of GET /api/v1/resources/packs and the resourcePacks segment of the sources
// report: the directory that is read, the archives in load order and what went wrong.
type PackReport struct {
	Directory string    `json:"directory"`
	CacheRoot string    `json:"cacheRoot"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Packs     []Pack    `json:"packs"`
	Warnings  []string  `json:"warnings,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// packEntry is one archive of the stack. It is immutable once a scan produced it, which is what lets
// a scan run without holding the lock the render path takes.
type packEntry struct {
	file    string
	addedAt time.Time
	root    string
	files   int
	format  int
	status  string
	err     string
}

type packManifest struct {
	Version int                 `json:"version"`
	Packs   []packManifestEntry `json:"packs"`
}

type packManifestEntry struct {
	File    string    `json:"file"`
	AddedAt time.Time `json:"addedAt"`
}

// packStore owns the scanned pack list. Scans do their I/O outside mu; only swapping the result,
// publishing the report and bumping the revision happen under the lock, so a reload never blocks a
// running render for the duration of an unzip.
type packStore struct {
	cfg          config.Config
	dir          string
	cacheRoot    string
	manifestPath string

	// scanMu serializes scans, so the startup load and the watcher cannot interleave.
	scanMu sync.Mutex

	mu       sync.RWMutex
	entries  []packEntry
	revision int64
	report   PackReport
	// scanned is the directory signature the loaded stack was built from. The watcher compares
	// against it instead of sampling the directory when its goroutine happens to start, so an archive
	// dropped in between the startup load and the first poll is still noticed.
	scanned string
}

func newPackStore(cfg config.Config) *packStore {
	directory := cfg.PackDir()
	return &packStore{
		cfg:          cfg,
		dir:          directory,
		cacheRoot:    filepath.Join(cfg.DataDir, "resources", packCacheDir),
		manifestPath: filepath.Join(cfg.DataDir, "states", "resource-packs.json"),
		report:       PackReport{Directory: directory, CacheRoot: filepath.Join(cfg.DataDir, "resources", packCacheDir), Status: PackStatusReady, Packs: []Pack{}},
	}
}

// Report answers the current stack without rescanning the directory.
func (s *packStore) Report() PackReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePackReport(s.report)
}

// Roots is the ordered list handed to the renderer. A root whose tree disappeared is skipped instead
// of being passed on as a path that answers every lookup with the missing texture.
func (s *packStore) Roots() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.entries) == 0 {
		return nil
	}
	roots := make([]string, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.status != PackLoaded || entry.root == "" {
			continue
		}
		if info, err := os.Stat(entry.root); err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, entry.root)
	}
	return roots
}

// Revision counts the stack changes. The fallback renderer caches its font per root list, and a pack
// replaced by a different one under the same name would otherwise keep stale glyphs alive.
func (s *packStore) Revision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// Reload rescans the directory. prune removes cache trees no pack refers to any more; it is only done
// at startup, because a render that started before the scan may still be reading one of them.
func (s *packStore) Reload(prune bool) (PackReport, error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	entries, warnings, err := s.scan(prune)
	scanned := s.fingerprint()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		// The directory itself could not be read. Keep the previous stack renderable and report the
		// reason, instead of answering every render with vanilla resources that look correct.
		s.report.Status = PackStatusError
		s.report.Error = err.Error()
		s.report.Warnings = warnings
		s.report.UpdatedAt = time.Now().UTC()
		return clonePackReport(s.report), err
	}
	s.entries = entries
	s.revision++
	s.scanned = scanned
	s.report = s.buildReportLocked(warnings)
	s.persistLocked()
	return clonePackReport(s.report), nil
}

// SetOrder replaces the load order. The list must name every loaded or failed archive of the report
// exactly once, which makes the request the inverse of what GET publishes.
func (s *packStore) SetOrder(order []string) (PackReport, error) {
	cleaned := make([]string, 0, len(order))
	seen := make(map[string]struct{}, len(order))
	for _, name := range order {
		name = strings.TrimSpace(name)
		if name == "" {
			return s.Report(), errors.New("resource pack order contains an empty file name")
		}
		if _, duplicate := seen[name]; duplicate {
			return s.Report(), fmt.Errorf("resource pack %q is listed twice", name)
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(cleaned) != len(s.entries) {
		return clonePackReport(s.report), fmt.Errorf("resource pack order lists %d archives, the directory has %d", len(cleaned), len(s.entries))
	}
	byName := make(map[string]packEntry, len(s.entries))
	for _, entry := range s.entries {
		byName[entry.file] = entry
	}
	reordered := make([]packEntry, 0, len(cleaned))
	for _, name := range cleaned {
		entry, ok := byName[name]
		if !ok {
			return clonePackReport(s.report), fmt.Errorf("unknown resource pack %q", name)
		}
		reordered = append(reordered, entry)
	}
	s.entries = reordered
	s.revision++
	s.report = s.buildReportLocked(append([]string(nil), s.report.Warnings...))
	s.persistLocked()
	return clonePackReport(s.report), nil
}

// Watch polls the directory and reloads the stack once an observed change has settled. A file that
// is still being copied is therefore never handed to the extractor.
func (s *packStore) Watch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	last := s.loadedSignature()
	pending := ""
	lastError := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := s.fingerprint()
			if current == last {
				pending = ""
				continue
			}
			if pending != current {
				pending = current
				continue
			}
			report, err := s.Reload(false)
			if err != nil {
				// A directory that stays unreadable is retried on the next cycle, but reported once:
				// repeating the same warning every interval would bury everything else in the log.
				if err.Error() != lastError {
					slog.Warn("resource packs could not be reloaded", "directory", s.dir, "error", err)
					lastError = err.Error()
				}
			} else {
				slog.Info("resource packs reloaded", "directory", report.Directory, "packs", len(report.Packs))
				lastError = ""
			}
			last = s.loadedSignature()
			pending = ""
		}
	}
}

// loadedSignature is the signature the last successful scan observed.
func (s *packStore) loadedSignature() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanned
}

// fingerprint is the cheap signature the watcher compares: every candidate's name, size and
// modification time. The directory's own timestamp is deliberately left out — unzipping into the
// cache touches the data directory, and including it would make the watcher reload forever.
func (s *packStore) fingerprint() string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return ""
	}
	type item struct {
		name string
		size int64
		mod  int64
	}
	items := make([]item, 0, len(entries))
	for _, entry := range entries {
		if !isPackCandidate(entry) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		items = append(items, item{name: entry.Name(), size: info.Size(), mod: info.ModTime().UnixNano()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	var signature strings.Builder
	for _, value := range items {
		fmt.Fprintf(&signature, "%s|%d|%d\n", value.name, value.size, value.mod)
	}
	return signature.String()
}

// scan builds the ordered stack from disk. It never returns an error for a single unreadable pack:
// that archive is reported as failed and the rest of the stack still renders.
func (s *packStore) scan(prune bool) ([]packEntry, []string, error) {
	warnings := make([]string, 0, 4)
	warn := func(format string, values ...any) {
		if len(warnings) < maxPackWarnings {
			warnings = append(warnings, fmt.Sprintf(format, values...))
		}
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return nil, warnings, fmt.Errorf("create the resource pack directory: %w", err)
	}
	listing, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, warnings, fmt.Errorf("read the resource pack directory: %w", err)
	}
	type candidate struct {
		name    string
		path    string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(listing))
	byName := make(map[string]candidate, len(listing))
	for _, entry := range listing {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !isPackCandidate(entry) {
			warn("ignored %q: only .zip resource packs are read", name)
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			warn("ignored %q: %v", name, infoErr)
			continue
		}
		value := candidate{name: name, path: filepath.Join(s.dir, name), modTime: info.ModTime()}
		candidates = append(candidates, value)
		byName[name] = value
	}
	manifest := s.readManifest(warn)
	ordered := make([]candidate, 0, len(candidates))
	addedAt := make(map[string]time.Time, len(candidates))
	for _, recorded := range manifest.Packs {
		value, ok := byName[recorded.File]
		if !ok {
			continue
		}
		delete(byName, recorded.File)
		ordered = append(ordered, value)
		stamp := recorded.AddedAt
		if stamp.IsZero() {
			stamp = value.modTime
		}
		addedAt[value.name] = stamp
	}
	fresh := make([]candidate, 0, len(byName))
	for _, value := range byName {
		fresh = append(fresh, value)
	}
	// A pack discovered for the first time keeps its creation order: modification time first, then the
	// file name. It is appended after every known pack, so dropping an archive in never silently
	// changes the precedence of the packs already there.
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].modTime.Equal(fresh[j].modTime) {
			return fresh[i].name < fresh[j].name
		}
		return fresh[i].modTime.Before(fresh[j].modTime)
	})
	now := time.Now().UTC()
	for _, value := range fresh {
		ordered = append(ordered, value)
		addedAt[value.name] = now
	}
	if len(ordered) > maxResourcePacks {
		warn("only the first %d of %d resource packs are loaded", maxResourcePacks, len(ordered))
		ordered = ordered[:maxResourcePacks]
	}
	entries := make([]packEntry, 0, len(ordered))
	for _, value := range ordered {
		entries = append(entries, s.prepare(value.name, value.path, addedAt[value.name]))
	}
	if prune {
		s.prune(entries, warn)
	}
	return entries, warnings, nil
}

// prepare turns one archive into a stack entry: unzip (or reuse the content addressed cache), read
// pack.mcmeta and record what the report publishes.
func (s *packStore) prepare(name, path string, addedAt time.Time) packEntry {
	entry := packEntry{file: name, addedAt: addedAt, status: PackFailed}
	hash, err := fileSHA1(path)
	if err != nil {
		entry.err = err.Error()
		return entry
	}
	root := filepath.Join(s.cacheRoot, "pack-"+hash[:16])
	if err = s.extract(path, root); err != nil {
		entry.err = err.Error()
		_ = os.RemoveAll(root)
		return entry
	}
	files, countErr := archiveFileCount(path)
	if countErr != nil {
		entry.err = countErr.Error()
		return entry
	}
	metadata, metadataErr := readPackMetadata(root)
	if metadataErr != nil {
		// A pack the client would refuse is left out of the stack; its unzipped tree is removed so the
		// next scan starts from the archive again.
		entry.err = metadataErr.Error()
		_ = os.RemoveAll(root)
		return entry
	}
	if !metadata.compatible() {
		min, max := metadata.formatRange()
		entry.err = fmt.Sprintf("pack format %d-%d is incompatible with Minecraft 1.21.11 (format %d)", min, max, clientPackFormat)
		_ = os.RemoveAll(root)
		return entry
	}
	entry.root = root
	entry.files = files
	entry.format = metadata.packFormat()
	entry.status = PackLoaded
	return entry
}

// extract publishes the archive under its content derived directory. The presence of pack.mcmeta is
// the marker of a finished extraction, which is what makes a restart cost one hash and no unzip.
func (s *packStore) extract(path, target string) error {
	if info, err := os.Stat(filepath.Join(target, "pack.mcmeta")); err == nil && info.Mode().IsRegular() {
		return nil
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("invalid zip archive: %w", err)
	}
	defer archive.Close()
	if err = os.MkdirAll(s.cacheRoot, 0o750); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(s.cacheRoot, ".extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err = extractZipEntries(archive.File, staging, s.cfg.MaxArchiveFiles, s.cfg.MaxArchiveBytes); err != nil {
		return err
	}
	if err = os.RemoveAll(target); err != nil {
		return err
	}
	return os.Rename(staging, target)
}

// prune drops cache trees no archive refers to any more. It runs at startup only: a render that began
// before this scan may still hold one of those paths.
func (s *packStore) prune(entries []packEntry, warn func(string, ...any)) {
	keep := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.root != "" {
			keep[filepath.Clean(entry.root)] = struct{}{}
		}
	}
	cached, err := os.ReadDir(s.cacheRoot)
	if err != nil {
		return
	}
	for _, candidate := range cached {
		path := filepath.Join(s.cacheRoot, candidate.Name())
		if _, used := keep[filepath.Clean(path)]; used {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			warn("could not remove the stale pack cache %q: %v", candidate.Name(), err)
		}
	}
}

func (s *packStore) buildReportLocked(warnings []string) PackReport {
	report := PackReport{
		Directory: s.dir,
		CacheRoot: s.cacheRoot,
		Status:    PackStatusReady,
		Packs:     make([]Pack, 0, len(s.entries)),
		UpdatedAt: time.Now().UTC(),
	}
	if len(warnings) > 0 {
		report.Warnings = append([]string(nil), warnings...)
	}
	for index, entry := range s.entries {
		report.Packs = append(report.Packs, Pack{
			File:       entry.file,
			Priority:   index + 1,
			AddedAt:    entry.addedAt,
			Root:       entry.root,
			Files:      entry.files,
			PackFormat: entry.format,
			Status:     entry.status,
			Error:      entry.err,
		})
	}
	return report
}

func (s *packStore) readManifest(warn func(string, ...any)) packManifest {
	data, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return packManifest{Version: packManifestVersion}
	}
	var manifest packManifest
	if err = json.Unmarshal(data, &manifest); err != nil || manifest.Version != packManifestVersion {
		warn("ignored the unreadable resource pack order manifest, rebuilding the order")
		return packManifest{Version: packManifestVersion}
	}
	return manifest
}

func (s *packStore) persistLocked() {
	manifest := packManifest{Version: packManifestVersion, Packs: make([]packManifestEntry, 0, len(s.entries))}
	for _, entry := range s.entries {
		manifest.Packs = append(manifest.Packs, packManifestEntry{File: entry.file, AddedAt: entry.addedAt})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return
	}
	if err = writeAtomic(s.manifestPath, append(data, '\n'), 0o640); err != nil {
		slog.Warn("could not persist the resource pack order", "path", s.manifestPath, "error", err)
	}
}

func clonePackReport(report PackReport) PackReport {
	clone := report
	clone.Packs = append([]Pack(nil), report.Packs...)
	clone.Warnings = append([]string(nil), report.Warnings...)
	if clone.Packs == nil {
		clone.Packs = []Pack{}
	}
	return clone
}

// isPackCandidate is the one place that decides what the directory contains: a visible .zip file.
func isPackCandidate(entry os.DirEntry) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".") {
		return false
	}
	info, err := entry.Info()
	return err == nil && info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(name), ".zip")
}

func fileSHA1(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha1.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func archiveFileCount(path string) (int, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer archive.Close()
	return len(archive.File), nil
}
