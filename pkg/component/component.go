package component

import (
	"github.com/fogleman/gg"
	"math"
)

// Text Component

const TextTag = '§'

type Component struct {
	origin   string
	charList []*Char
}

func NewComponent(s string) *Component {
	return &Component{
		origin:   s,
		charList: nil,
	}
}

// Compute char list width with suggestCtx, returns (nameWidth, startY)
func (c *Component) Compute(suggestCtx *gg.Context, boldOffset float64) (nameWidth float64, maxHeight float64) {
	if c.charList == nil {
		c.Parse()
	}
	nameWidth = 0.0
	maxHeight = 0.0
	for _, char := range c.charList {
		w, h := suggestCtx.MeasureString(string(char.Content))
		nameWidth += w
		maxHeight = math.Max(h, maxHeight)
		switch char.Type {
		case Bold:
			{
				nameWidth += boldOffset
				break
			}
		case Italic:
			{
				nameWidth += 2
				break
			}
		}
	}
	return nameWidth, maxHeight
}

func (c *Component) Parse() []*Char {
	if c.charList != nil {
		return c.charList
	}

	c.charList = make([]*Char, 0)
	var codeTag = false
	var color = DefaultColor
	var charType = Default
	for _, charInt := range c.origin {
		if charInt == TextTag {
			codeTag = true
			continue
		}
		if !codeTag {
			c.charList = append(c.charList, &Char{
				Type:    charType,
				Color:   color,
				Content: charInt,
			})
			continue
		}
		codeTag = false // reset
		if rgba := ColorMapping[charInt]; rgba != nil {
			color = rgba
			continue
		}
		if newCharType := FormatMapping[charInt]; newCharType != "" {
			if newCharType == Reset {
				newCharType = Default
			}
			charType = newCharType
			continue
		}
	}
	return c.charList
}
