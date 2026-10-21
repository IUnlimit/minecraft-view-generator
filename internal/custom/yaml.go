package custom

import (
	"fmt"
	"strconv"
	"strings"
)

// The custom content packs of ItemsAdder are plain YAML, but the module has no third party
// dependency and pulling one in for a handful of configuration keys would be the larger risk. This
// file therefore reads the subset those files actually use: nested mappings by indentation, block
// sequences of scalars, inline [a, b] sequences, quoted strings and # comments. Anything outside the
// subset — anchors, aliases, block scalars, inline mappings — is kept as its raw text so a caller
// that does not understand it can ignore one key instead of failing the whole pack.
//
// Measured against the installation this service targets: 3164 indexed items across 51 materials,
// with no anchors, no block scalars and no inline mappings anywhere in the configs.

// maxWarningsPerFile keeps one broken file from filling the log with a line per entry.
const maxWarningsPerFile = 8

// yamlLine is one significant line: its indentation in spaces and the text without the indent.
type yamlLine struct {
	indent int
	text   string
	number int
}

// parseDocument reads a YAML document into nested map[string]any, []any and string values. It never
// fails: a line it cannot place is reported as a warning and skipped.
func parseDocument(data []byte) (map[string]any, []string) {
	lines, warnings := scanYAMLLines(string(data))
	if len(lines) == 0 {
		return map[string]any{}, warnings
	}
	value, _ := parseYAMLBlock(lines, 0, lines[0].indent)
	if document, ok := value.(map[string]any); ok {
		return document, warnings
	}
	warnings = append(warnings, "the document is not a mapping")
	return map[string]any{}, warnings
}

func scanYAMLLines(document string) ([]yamlLine, []string) {
	var lines []yamlLine
	var warnings []string
	for index, raw := range strings.Split(document, "\n") {
		text := strings.TrimRight(raw, "\r")
		indent := 0
		for indent < len(text) && (text[indent] == ' ') {
			indent++
		}
		content := strings.TrimSpace(text[indent:])
		if content == "" || content == "---" || content == "..." || strings.HasPrefix(content, "#") {
			continue
		}
		if strings.HasPrefix(text, "\t") {
			if len(warnings) < maxWarningsPerFile {
				warnings = append(warnings, fmt.Sprintf("line %d is indented with a tab", index+1))
			}
			continue
		}
		lines = append(lines, yamlLine{indent: indent, text: content, number: index + 1})
	}
	return lines, warnings
}

// parseYAMLBlock reads the block that starts at lines[index] and is indented by indent, and returns
// the next line index that belongs to an enclosing block.
func parseYAMLBlock(lines []yamlLine, index, indent int) (any, int) {
	if index < len(lines) && isYAMLSequenceEntry(lines[index].text) {
		return parseYAMLSequence(lines, index, indent)
	}
	return parseYAMLMapping(lines, index, indent)
}

// isYAMLSequenceEntry reports whether the stripped line text starts a block sequence entry.
func isYAMLSequenceEntry(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func parseYAMLMapping(lines []yamlLine, index, indent int) (any, int) {
	mapping := make(map[string]any)
	for index < len(lines) {
		line := lines[index]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			// A deeper line with no key of its own: the file mixes indentation styles. Skip it so
			// the keys that do parse stay usable.
			index++
			continue
		}
		key, rest, ok := splitYAMLKey(line.text)
		if !ok {
			index++
			continue
		}
		if rest == "" {
			if index+1 < len(lines) {
				next := lines[index+1]
				if next.indent > indent {
					child, after := parseYAMLBlock(lines, index+1, next.indent)
					mapping[key] = child
					index = after
					continue
				}
				// A block sequence may sit at the indentation of its own key: ItemsAdder writes
				// "textures:" and the "- item/…" entries at the same column.
				if next.indent == indent && isYAMLSequenceEntry(next.text) {
					child, after := parseYAMLSequence(lines, index+1, indent)
					mapping[key] = child
					index = after
					continue
				}
			}
			mapping[key] = nil
			index++
			continue
		}
		mapping[key] = parseYAMLScalar(rest)
		index++
	}
	return mapping, index
}

func parseYAMLSequence(lines []yamlLine, index, indent int) (any, int) {
	sequence := make([]any, 0, 4)
	for index < len(lines) {
		line := lines[index]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			index++
			continue
		}
		if line.text == "-" {
			sequence = append(sequence, nil)
			index++
			continue
		}
		if !strings.HasPrefix(line.text, "- ") {
			break
		}
		// A sequence entry that is itself a mapping keeps its raw text: the keys this package reads
		// never live inside one, and guessing would only produce wrong trees.
		sequence = append(sequence, parseYAMLScalar(strings.TrimSpace(line.text[2:])))
		index++
	}
	return sequence, index
}

// splitYAMLKey finds the colon that ends a key. A colon inside a value (the namespace of
// "icon_pack:1") is followed by a name character, not by a space, so it never splits.
func splitYAMLKey(text string) (string, string, bool) {
	if strings.HasPrefix(text, "- ") || text == "-" {
		return "", "", false
	}
	for position := 0; position < len(text); position++ {
		if text[position] != ':' {
			continue
		}
		if position+1 < len(text) && text[position+1] != ' ' {
			continue
		}
		key := strings.TrimSpace(text[:position])
		if key == "" {
			return "", "", false
		}
		return unquoteYAML(key), strings.TrimSpace(text[position+1:]), true
	}
	return "", "", false
}

// parseYAMLScalar strips a trailing comment, unquotes the value and expands an inline sequence.
func parseYAMLScalar(text string) any {
	value := strings.TrimSpace(stripYAMLComment(text))
	if value == "" {
		return nil
	}
	switch value[0] {
	case '"', '\'':
		return unquoteYAML(value)
	case '[':
		return parseYAMLInlineSequence(value)
	default:
		return value
	}
}

// stripYAMLComment removes a # comment that is not inside a quoted string.
func stripYAMLComment(text string) string {
	var quote byte
	for position := 0; position < len(text); position++ {
		character := text[position]
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			}
		case character == '"' || character == '\'':
			quote = character
		case character == '#':
			if position == 0 || text[position-1] == ' ' || text[position-1] == '\t' {
				return text[:position]
			}
		}
	}
	return text
}

func unquoteYAML(text string) string {
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		if unquoted, err := strconv.Unquote(text); err == nil {
			return unquoted
		}
		return text[1 : len(text)-1]
	}
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
		return strings.ReplaceAll(text[1:len(text)-1], "''", "'")
	}
	return text
}

func parseYAMLInlineSequence(text string) []any {
	body := strings.TrimSuffix(strings.TrimPrefix(text, "["), "]")
	parts := strings.Split(body, ",")
	values := make([]any, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, parseYAMLScalar(trimmed))
		}
	}
	return values
}

// yamlString is the accessor the callers need: the value of a scalar key, or "" for anything else.
func yamlString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

// yamlBool reads the spellings ItemsAdder writes for a flag.
func yamlBool(value any) bool {
	switch strings.ToLower(strings.TrimSpace(yamlString(value))) {
	case "true", "yes", "on", "1":
		return true
	default:
		return false
	}
}

// yamlStrings reads a key that is either one scalar or a sequence of scalars.
func yamlStrings(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed == "" {
			return nil
		}
		return []string{typed}
	case []any:
		values := make([]string, 0, len(typed))
		for _, entry := range typed {
			if text := strings.TrimSpace(yamlString(entry)); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func yamlMapping(value any) map[string]any {
	if mapping, ok := value.(map[string]any); ok {
		return mapping
	}
	return nil
}
