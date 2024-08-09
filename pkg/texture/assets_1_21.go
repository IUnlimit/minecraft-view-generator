package texture

const (
	Icon = Root + "/gui/sprites/icon"
)

type Asset1x21y struct {
}

// GetPing ping < 0 is considered disconnect
func (a *Asset1x21y) GetPing(ping int) *Texture {
	if ping < 0 {
		return format(Icon, "/ping_unknown", ImageSuffix)
	} else if ping < 150 {
		return format(Icon, "/ping_5", ImageSuffix)
	} else if ping < 300 {
		return format(Icon, "/ping_4", ImageSuffix)
	} else if ping < 600 {
		return format(Icon, "/ping_3", ImageSuffix)
	} else if ping < 1000 {
		return format(Icon, "/ping_2", ImageSuffix)
	} else {
		return format(Icon, "/ping_1", ImageSuffix)
	}
}
