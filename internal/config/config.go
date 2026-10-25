package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config contains process-wide settings. Resource downloads are only allowed
// against the explicitly configured upstream hosts.
type Config struct {
	ListenAddr string
	DataDir    string
	// ResourcePackRoot overrides the directory the user resource packs are read from. Empty means the
	// default next to the data directory, which is also what keeps a config built without FromEnv
	// (tests, embedders) from pointing at the checked in ./data tree.
	ResourcePackRoot string
	// ResourcePackWatchInterval is how often the resource pack directory is compared against its last
	// loaded state. Zero disables the watcher, so new archives are only picked up by a restart.
	ResourcePackWatchInterval time.Duration
	// Debug raises the log level to debug, which prints every received request payload.
	Debug                  bool
	MaxBodyBytes           int64
	MaxImageWidth          int
	MaxImageHeight         int
	MaxRenderDuration      time.Duration
	MaxConcurrentRenders   int
	HTTPTimeout            time.Duration
	CORSAllowedOrigins     []string
	APIKey                 string
	RequireResources       bool
	AllowFallbackRenderer  bool
	RendererCommand        string
	RendererMaxOutputBytes int64
	MojangManifestURL      string
	AddonVersionsURL       string
	AddonResourceURL       string
	AllowedResourceHosts   []string
	AddonEnabled           bool
	MaxDownloadBytes       int64
	MaxArchiveFiles        int
	MaxArchiveBytes        int64
	// CustomContentsDir holds user supplied item packs in the layout of the ItemsAdder contents
	// directory. An absent directory is an empty index, not an error: the service keeps rendering
	// vanilla items.
	CustomContentsDir string
	// CustomItemsCache is the ItemsAdder items_ids_cache.yml that maps a base material plus a
	// custom_model_data value to a namespaced item id.
	CustomItemsCache string
	// MaxCustomItems bounds the indexed entries, so a runaway directory cannot exhaust memory.
	MaxCustomItems int
	// MaxCustomTextureBytes bounds one inline texture of a custom item.
	MaxCustomTextureBytes int
	// MaxCustomLayers bounds how many textures one custom item may inline.
	MaxCustomLayers int
}

func Default() Config {
	return Config{
		ListenAddr:                ":8080",
		DataDir:                   "./data",
		ResourcePackRoot:          "",
		ResourcePackWatchInterval: time.Second,
		MaxBodyBytes:              2 << 20,
		MaxImageWidth:             1600,
		MaxImageHeight:            1200,
		MaxRenderDuration:         15 * time.Second,
		MaxConcurrentRenders:      4,
		HTTPTimeout:               30 * time.Second,
		CORSAllowedOrigins:        []string{"*"},
		RequireResources:          true,
		RendererCommand:           "./renderer-backend/run.sh",
		RendererMaxOutputBytes:    64 << 20,
		MojangManifestURL:         "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json",
		AddonVersionsURL:          "https://api.loohpjames.com/spigot/plugins/interactivechatdiscordsrvaddon/versions",
		AddonResourceURL:          "https://api.loohpjames.com/spigot/plugins/interactivechatdiscordsrvaddon/versions",
		AllowedResourceHosts:      []string{"piston-meta.mojang.com", "piston-data.mojang.com", "resources.download.minecraft.net", "api.loohpjames.com"},
		AddonEnabled:              true,
		MaxDownloadBytes:          512 << 20,
		MaxArchiveFiles:           100_000,
		MaxArchiveBytes:           2 << 30,
		CustomContentsDir:         filepath.Join("./data", "contents"),
		CustomItemsCache:          filepath.Join("./data", "items_ids_cache.yml"),
		MaxCustomItems:            50_000,
		MaxCustomTextureBytes:     8 << 20,
		MaxCustomLayers:           16,
	}
}

func FromEnv() Config {
	cfg := Default()
	cfg.ListenAddr = envString("LISTEN_ADDR", cfg.ListenAddr)
	cfg.DataDir = envString("DATA_DIR", cfg.DataDir)
	cfg.ResourcePackRoot = envString("RESOURCE_PACK_ROOT", cfg.ResourcePackRoot)
	cfg.ResourcePackWatchInterval = envDuration("RESOURCE_PACK_WATCH_INTERVAL", cfg.ResourcePackWatchInterval)
	// The custom content paths default next to the data directory, so a deployment only has to move
	// DATA_DIR to carry its packs along.
	cfg.CustomContentsDir = envString("CUSTOM_CONTENTS_DIR", filepath.Join(cfg.DataDir, "contents"))
	cfg.CustomItemsCache = envString("CUSTOM_ITEMS_CACHE", filepath.Join(cfg.DataDir, "items_ids_cache.yml"))
	cfg.MaxCustomItems = envInt("MAX_CUSTOM_ITEMS", cfg.MaxCustomItems)
	cfg.MaxCustomTextureBytes = envInt("MAX_CUSTOM_TEXTURE_BYTES", cfg.MaxCustomTextureBytes)
	cfg.MaxCustomLayers = envInt("MAX_CUSTOM_LAYERS", cfg.MaxCustomLayers)
	cfg.APIKey = os.Getenv("API_KEY")
	cfg.Debug = envBool("DEBUG", cfg.Debug)
	cfg.AllowFallbackRenderer = envBool("ALLOW_FALLBACK_RENDERER", cfg.AllowFallbackRenderer)
	cfg.RendererCommand = envString("RENDERER_COMMAND", cfg.RendererCommand)
	cfg.RendererMaxOutputBytes = envInt64("RENDERER_MAX_OUTPUT_BYTES", cfg.RendererMaxOutputBytes)
	cfg.MojangManifestURL = envString("MOJANG_MANIFEST_URL", cfg.MojangManifestURL)
	cfg.AddonVersionsURL = envString("ADDON_VERSIONS_URL", cfg.AddonVersionsURL)
	cfg.AddonResourceURL = envString("ADDON_RESOURCE_URL", cfg.AddonResourceURL)
	cfg.CORSAllowedOrigins = envCSV("CORS_ALLOWED_ORIGINS", cfg.CORSAllowedOrigins)
	cfg.AllowedResourceHosts = envCSV("ALLOWED_RESOURCE_HOSTS", cfg.AllowedResourceHosts)
	cfg.AddonEnabled = envBool("ADDON_ENABLED", cfg.AddonEnabled)
	cfg.RequireResources = envBool("REQUIRE_RESOURCES", cfg.RequireResources)
	cfg.MaxBodyBytes = envInt64("MAX_BODY_BYTES", cfg.MaxBodyBytes)
	cfg.MaxImageWidth = envInt("MAX_IMAGE_WIDTH", cfg.MaxImageWidth)
	cfg.MaxImageHeight = envInt("MAX_IMAGE_HEIGHT", cfg.MaxImageHeight)
	cfg.MaxConcurrentRenders = envInt("MAX_CONCURRENT_RENDERS", cfg.MaxConcurrentRenders)
	cfg.MaxDownloadBytes = envInt64("MAX_DOWNLOAD_BYTES", cfg.MaxDownloadBytes)
	cfg.MaxArchiveFiles = envInt("MAX_ARCHIVE_FILES", cfg.MaxArchiveFiles)
	cfg.MaxArchiveBytes = envInt64("MAX_ARCHIVE_BYTES", cfg.MaxArchiveBytes)
	cfg.HTTPTimeout = envDuration("HTTP_TIMEOUT", cfg.HTTPTimeout)
	cfg.MaxRenderDuration = envDuration("MAX_RENDER_DURATION", cfg.MaxRenderDuration)
	return cfg
}

// PackDir is the directory the user resource packs are read from. It defaults to resourcepacks/ next
// to the data directory — the name Minecraft itself gives its pack folder — rather than to a literal
// Default() value, so a config whose DataDir is changed after Default() (tests, embedders) still
// keeps its packs inside that data directory.
func (c Config) PackDir() string {
	if root := strings.TrimSpace(c.ResourcePackRoot); root != "" {
		return root
	}
	return filepath.Join(c.DataDir, "resourcepacks")
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("listen address is empty")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data directory is empty")
	}
	if c.ResourcePackWatchInterval < 0 {
		return fmt.Errorf("resource pack watch interval must not be negative")
	}
	if strings.TrimSpace(c.CustomContentsDir) == "" || strings.TrimSpace(c.CustomItemsCache) == "" {
		return fmt.Errorf("custom content paths must not be empty")
	}
	if c.MaxCustomItems <= 0 || c.MaxCustomTextureBytes <= 0 || c.MaxCustomLayers <= 0 || c.MaxCustomLayers > 64 {
		return fmt.Errorf("custom content limits must be positive and at most 64 layers")
	}
	if c.MaxBodyBytes <= 0 || c.MaxBodyBytes > 64<<20 {
		return fmt.Errorf("max body bytes must be between 1 and 67108864")
	}
	if c.MaxImageWidth <= 0 || c.MaxImageHeight <= 0 || c.MaxImageWidth > 4096 || c.MaxImageHeight > 4096 {
		return fmt.Errorf("image dimensions must be between 1 and 4096")
	}
	if c.MaxConcurrentRenders <= 0 || c.MaxConcurrentRenders > 128 {
		return fmt.Errorf("max concurrent renders must be between 1 and 128")
	}
	if c.HTTPTimeout <= 0 || c.MaxRenderDuration <= 0 {
		return fmt.Errorf("timeouts must be positive")
	}
	if c.MaxDownloadBytes <= 0 || c.MaxArchiveFiles <= 0 || c.MaxArchiveBytes <= 0 {
		return fmt.Errorf("resource limits must be positive")
	}
	if c.RendererMaxOutputBytes <= 0 || c.RendererMaxOutputBytes > 256<<20 {
		return fmt.Errorf("renderer max output bytes must be between 1 and 268435456")
	}
	if c.MojangManifestURL == "" || c.AddonVersionsURL == "" || c.AddonResourceURL == "" {
		return fmt.Errorf("resource endpoints must not be empty")
	}
	return nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envCSV(name string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
