package component

import "image/color"

type CharType string

const (
	Default       = CharType("Default")
	Obfuscated    = CharType("Obfuscated")
	Bold          = CharType("Bold")
	StrikeThrough = CharType("StrikeThrough")
	Underline     = CharType("Underline")
	Italic        = CharType("Italic")
	Reset         = CharType("Reset")
)

type Char struct {
	// 字符类型
	Type CharType
	// 字符颜色
	Color *color.RGBA
	// 字符
	Content rune
}
