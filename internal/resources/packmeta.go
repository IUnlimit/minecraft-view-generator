package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// clientPackFormat is the resource pack format of the Minecraft version this renderer targets: it is
// pack_version.resource_major of the client JAR's version.json (1.21.11 → 75). It decides which of a
// pack's overlays are stacked, so upgrading the target version means bumping this constant.
const clientPackFormat = 75

// supportedPackFormatMax bounds the pack formats this renderer accepts. A pack is refused only when
// its whole declared range lies above it: 1.21.9 spellt "this runs on anything from here on" as e.g.
// max_format [1000, 0], which must not be read as "needs a future client".
const supportedPackFormatMax = 100

// packOverlay is one entry of pack.mcmeta's overlays.entries: a directory that is stacked on top of
// the pack root while the client's format is inside [Min, Max].
type packOverlay struct {
	Directory string
	Min       int
	Max       int
}

// packMetadata is what pack.mcmeta declares. Zero means "not declared" and is resolved by
// formatRange, so a pack that spells its format in any of the client's ways still yields a range.
type packMetadata struct {
	Format   int
	Min      int
	Max      int
	Overlays []packOverlay
}

// readPackMetadata parses pack.mcmeta. Every version's spelling of a format is accepted here — a
// plain major number, a [major, minor] tuple, supported_formats as an object or as a [min, max]
// array — because a pack that merely uses newer syntax is still readable, and refusing it would drop
// its whole tree from the stack.
func readPackMetadata(root string) (packMetadata, error) {
	data, err := os.ReadFile(filepath.Join(root, "pack.mcmeta"))
	if err != nil {
		return packMetadata{}, fmt.Errorf("pack.mcmeta is required: %w", err)
	}
	var raw struct {
		Pack struct {
			PackFormat       json.RawMessage `json:"pack_format"`
			MinFormat        json.RawMessage `json:"min_format"`
			MaxFormat        json.RawMessage `json:"max_format"`
			SupportedFormats json.RawMessage `json:"supported_formats"`
		} `json:"pack"`
		Overlays struct {
			Entries []struct {
				Directory string          `json:"directory"`
				Formats   json.RawMessage `json:"formats"`
				MinFormat json.RawMessage `json:"min_format"`
				MaxFormat json.RawMessage `json:"max_format"`
			} `json:"entries"`
		} `json:"overlays"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return packMetadata{}, fmt.Errorf("invalid pack.mcmeta: %w", err)
	}
	var metadata packMetadata
	if metadata.Format, err = parsePackFormat(raw.Pack.PackFormat); err != nil {
		return packMetadata{}, fmt.Errorf("invalid pack.mcmeta: pack_format %w", err)
	}
	if metadata.Min, err = parsePackFormat(raw.Pack.MinFormat); err != nil {
		return packMetadata{}, fmt.Errorf("invalid pack.mcmeta: min_format %w", err)
	}
	if metadata.Max, err = parsePackFormat(raw.Pack.MaxFormat); err != nil {
		return packMetadata{}, fmt.Errorf("invalid pack.mcmeta: max_format %w", err)
	}
	// supported_formats is the older spelling of min_format/max_format, so an explicit field wins.
	supportedMin, supportedMax, rangeErr := parseFormatRange(raw.Pack.SupportedFormats)
	if rangeErr != nil {
		return packMetadata{}, fmt.Errorf("invalid pack.mcmeta: supported_formats %w", rangeErr)
	}
	if metadata.Min == 0 {
		metadata.Min = supportedMin
	}
	if metadata.Max == 0 {
		metadata.Max = supportedMax
	}
	min, max := metadata.formatRange()
	for _, entry := range raw.Overlays.Entries {
		directory := strings.TrimSpace(entry.Directory)
		if directory == "" {
			continue
		}
		overlayMin, overlayMax, overlayErr := overlayRange(entry.Formats, entry.MinFormat, entry.MaxFormat, min, max)
		if overlayErr != nil {
			return packMetadata{}, fmt.Errorf("invalid pack.mcmeta: overlay %q %w", directory, overlayErr)
		}
		metadata.Overlays = append(metadata.Overlays, packOverlay{Directory: directory, Min: overlayMin, Max: overlayMax})
	}
	return metadata, nil
}

// formatRange answers the interval the pack declares, falling back from min_format/max_format to the
// supported_formats pair, then to pack_format and finally to the formats every client understands.
func (m packMetadata) formatRange() (int, int) {
	min := m.Min
	if min == 0 {
		min = m.Format
	}
	if min == 0 {
		min = 1
	}
	max := m.Max
	if max == 0 {
		max = m.Format
	}
	if max == 0 {
		max = supportedPackFormatMax
	}
	if max < min {
		max = min
	}
	return min, max
}

// compatible reports whether the renderer's format is inside what the pack declares. Only a range
// that lies entirely above supportedPackFormatMax is refused.
func (m packMetadata) compatible() bool {
	min, _ := m.formatRange()
	return min <= supportedPackFormatMax
}

// packFormat is the number the report publishes: the declared pack_format, or the lower bound of the
// range for a pack that only spells min_format/max_format.
func (m packMetadata) packFormat() int {
	if m.Format != 0 {
		return m.Format
	}
	min, _ := m.formatRange()
	return min
}

// overlayRange resolves one overlay's interval: min_format/max_format win, then formats, then the
// pack's own range.
func overlayRange(formats, minFormat, maxFormat json.RawMessage, fallbackMin, fallbackMax int) (int, int, error) {
	min, max, err := parseFormatRange(formats)
	if err != nil {
		return 0, 0, err
	}
	declaredMin, err := parsePackFormat(minFormat)
	if err != nil {
		return 0, 0, err
	}
	declaredMax, err := parsePackFormat(maxFormat)
	if err != nil {
		return 0, 0, err
	}
	if declaredMin != 0 {
		min = declaredMin
	}
	if declaredMax != 0 {
		max = declaredMax
	}
	if min == 0 {
		min = fallbackMin
	}
	if max == 0 {
		max = fallbackMax
	}
	if max < min {
		max = min
	}
	return min, max, nil
}

// parsePackFormat reads one format field, which the client spells either as a plain major number or
// as a [major, minor] tuple. Zero means "not declared"; a declared non-positive major is refused
// because no client version uses one.
func parsePackFormat(raw json.RawMessage) (int, error) {
	if !hasPackValue(raw) {
		return 0, nil
	}
	var major int
	if err := json.Unmarshal(raw, &major); err == nil {
		if major < 1 {
			return 0, fmt.Errorf("%s is not a pack format", raw)
		}
		return major, nil
	}
	var tuple []int
	if err := json.Unmarshal(raw, &tuple); err != nil || len(tuple) != 2 || tuple[0] < 1 {
		return 0, fmt.Errorf("%s is not a pack format", raw)
	}
	return tuple[0], nil
}

// parseFormatRange reads a format interval. The client writes it as [min, max] from 1.21.9 on, as
// {min_inclusive, max_inclusive} before that, and as a single number when the bounds are equal.
func parseFormatRange(raw json.RawMessage) (int, int, error) {
	if !hasPackValue(raw) {
		return 0, 0, nil
	}
	switch raw[firstJSONIndex(raw)] {
	case '[':
		var tuple []json.RawMessage
		if err := json.Unmarshal(raw, &tuple); err != nil || len(tuple) != 2 {
			return 0, 0, fmt.Errorf("%s is not a [min_inclusive, max_inclusive] interval", raw)
		}
		min, err := parsePackFormat(tuple[0])
		if err != nil {
			return 0, 0, err
		}
		max, err := parsePackFormat(tuple[1])
		if err != nil {
			return 0, 0, err
		}
		return min, max, nil
	case '{':
		var object struct {
			Min json.RawMessage `json:"min_inclusive"`
			Max json.RawMessage `json:"max_inclusive"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return 0, 0, fmt.Errorf("%s is not a formats object", raw)
		}
		min, err := parsePackFormat(object.Min)
		if err != nil {
			return 0, 0, err
		}
		max, err := parsePackFormat(object.Max)
		if err != nil {
			return 0, 0, err
		}
		return min, max, nil
	}
	value, err := parsePackFormat(raw)
	if err != nil {
		return 0, 0, err
	}
	return value, value, nil
}

// hasPackValue reports whether a field was declared at all: a missing field and an explicit null are
// both "not declared".
func hasPackValue(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

// firstJSONIndex is the offset of the first non-space byte, so the caller can switch on the JSON
// kind without unmarshalling the value twice.
func firstJSONIndex(raw json.RawMessage) int {
	return len(raw) - len(bytes.TrimLeft(raw, " \t\r\n"))
}
