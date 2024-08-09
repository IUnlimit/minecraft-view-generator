package component

import "image/color"

var DefaultColor = &color.RGBA{R: 255, G: 255, B: 255, A: 255}

// ColorMapping Bukkit/Minecraft color codes mapped to Go's color.RGBA
var ColorMapping = map[rune]*color.RGBA{
	'0': {R: 0, G: 0, B: 0, A: 255},       // Black
	'1': {R: 0, G: 0, B: 170, A: 255},     // Dark Blue
	'2': {R: 0, G: 170, B: 0, A: 255},     // Dark Green
	'3': {R: 0, G: 170, B: 170, A: 255},   // Dark Aqua
	'4': {R: 170, G: 0, B: 0, A: 255},     // Dark Red
	'5': {R: 170, G: 0, B: 170, A: 255},   // Dark Purple
	'6': {R: 255, G: 170, B: 0, A: 255},   // Gold
	'7': {R: 170, G: 170, B: 170, A: 255}, // Gray
	'8': {R: 85, G: 85, B: 85, A: 255},    // Dark Gray
	'9': {R: 85, G: 85, B: 255, A: 255},   // Blue
	'a': {R: 85, G: 255, B: 85, A: 255},   // Green
	'b': {R: 85, G: 255, B: 255, A: 255},  // Aqua
	'c': {R: 255, G: 85, B: 85, A: 255},   // Red
	'd': {R: 255, G: 85, B: 255, A: 255},  // Light Purple
	'e': {R: 255, G: 255, B: 85, A: 255},  // Yellow
	'f': {R: 255, G: 255, B: 255, A: 255}, // White
	// g - u only for BE
}

var FormatMapping = map[rune]CharType{
	'k': Obfuscated,
	'l': Bold,
	'm': StrikeThrough,
	'n': Underline,
	'o': Italic,
	'r': Reset,
}
