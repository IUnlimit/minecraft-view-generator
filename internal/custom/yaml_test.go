package custom

import "testing"

// ItemsAdder writes block sequences at the same indentation as their key and saves the config with
// CRLF line endings, and the trailing space after "textures:" survives the round trip. Both shapes
// appear in the real customcrops pack, and a parser that only accepts a deeper sequence drops the
// texture list, which leaves the item without any texture to draw.
func TestParserReadsSameIndentSequences(t *testing.T) {
	document, warnings := parseDocument([]byte(
		"info:\r\n" +
			"  namespace: customcrops\r\n" +
			"items:\r\n" +
			"  speed_grow_2:\r\n" +
			"    display_name: display-name-speed_grow_2\r\n" +
			"    lore:\r\n" +
			"    - '&7[&x&6&6&9&9&f&f优质生长激素&7]'\r\n" +
			"    - '&x&0&0&f&f&c&c可以提高作物的生长速度'\r\n" +
			"    resource:\r\n" +
			"      generate: true\r\n" +
			"      material: PAPER\r\n" +
			"      textures: \r\n" +
			"      - item/fertilizer/speed_grow_2\r\n"))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	item := yamlMapping(yamlMapping(document["items"])["speed_grow_2"])
	if len(yamlStrings(item["lore"])) != 2 {
		t.Fatalf("lore = %#v, want 2 entries", item["lore"])
	}
	resource := yamlMapping(item["resource"])
	if !yamlBool(resource["generate"]) {
		t.Fatalf("generate = %#v, want true", resource["generate"])
	}
	textures := yamlStrings(resource["textures"])
	if len(textures) != 1 || textures[0] != "item/fertilizer/speed_grow_2" {
		t.Fatalf("textures = %#v, want the single texture of the item", textures)
	}
}

// A deeper block sequence and a same-indent sibling key must keep their own scopes: the sequence
// belongs to the key above it, and the next key still lands in the same mapping.
func TestParserKeepsSequenceAndFollowingKeyApart(t *testing.T) {
	document, warnings := parseDocument([]byte(`
items:
  crate:
    resource:
      textures:
        - item/top
        - item/side
      material: PAPER
  gem:
    resource:
      textures:
      - item/gem
      material: STICK
`))
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	items := yamlMapping(document["items"])
	crate := yamlMapping(yamlMapping(items["crate"])["resource"])
	if textures := yamlStrings(crate["textures"]); len(textures) != 2 {
		t.Fatalf("crate textures = %#v, want 2 entries", textures)
	}
	if material := yamlString(crate["material"]); material != "PAPER" {
		t.Fatalf("crate material = %q, want PAPER", material)
	}
	gem := yamlMapping(yamlMapping(items["gem"])["resource"])
	if textures := yamlStrings(gem["textures"]); len(textures) != 1 || textures[0] != "item/gem" {
		t.Fatalf("gem textures = %#v", textures)
	}
	if material := yamlString(gem["material"]); material != "STICK" {
		t.Fatalf("gem material = %q, want STICK", material)
	}
}
