package component

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

func (c *Component) ComputeWidth(func(s string) (w float64, h float64)) int {

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
			charType = newCharType
			continue
		}
	}
	return c.charList
}
