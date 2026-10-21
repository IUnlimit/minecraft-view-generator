package custom

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/illtamer/minecraft-view-generator2/internal/model"
)

// Cache is the ItemsAdder items_ids_cache.yml, the only file that ties a vanilla item to the custom
// model data value the server assigns to a custom item. Its shape is
//
//	PAPER:
//	  _iainternal:icon_left_blue: 10000
//	  fishing_pack:starfish: 1134
//
// so the lookup key is the base material plus the number, and the value is the namespaced item id.
// The numbers repeat across materials, which is why the material is part of the key.
type Cache struct {
	byKey map[string]string
	items int
}

// LoadCache reads the index from disk. A missing file is an empty index rather than an error: the
// service then simply renders vanilla items.
func LoadCache(path string) (*Cache, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Cache{byKey: map[string]string{}}, nil, nil
		}
		return nil, nil, err
	}
	cache, warnings := ParseCache(data)
	return cache, warnings, nil
}

// ParseCache reads the index from its bytes. Entries that are not a number are dropped with a
// warning, so one broken line cannot take the whole index down.
func ParseCache(data []byte) (*Cache, []string) {
	document, warnings := parseDocument(data)
	cache := &Cache{byKey: make(map[string]string)}
	for material, entries := range document {
		mapping := yamlMapping(entries)
		if mapping == nil {
			continue
		}
		normalised := model.MaterialKey(material)
		if normalised == "" {
			continue
		}
		for id, raw := range mapping {
			value, err := strconv.Atoi(strings.TrimSpace(yamlString(raw)))
			if err != nil {
				if len(warnings) < maxWarningsPerFile*4 {
					warnings = append(warnings, fmt.Sprintf("%s:%s is not a number, skipping it", material, id))
				}
				continue
			}
			if value < 0 {
				continue
			}
			key := model.CustomArtKey(normalised, value)
			if _, taken := cache.byKey[key]; taken {
				continue
			}
			cache.byKey[key] = id
			cache.items++
		}
	}
	return cache, warnings
}

// Lookup returns the namespaced item id indexed for a base material and custom model data value.
func (c *Cache) Lookup(material string, customData int) (string, bool) {
	return c.LookupKey(model.CustomArtKey(material, customData))
}

// LookupKey is Lookup for a key that is already built, which the art cache uses.
func (c *Cache) LookupKey(key string) (string, bool) {
	if c == nil {
		return "", false
	}
	id, ok := c.byKey[key]
	return id, ok
}

// Len is the number of indexed entries.
func (c *Cache) Len() int {
	if c == nil {
		return 0
	}
	return c.items
}
