package model

import "time"

type Config struct {
	Log       *Log       `yaml:"log"`
	Minecraft *Minecraft `yaml:"minecraft"`
}

type Log struct {
	ForceNew bool          `yaml:"force-new,omitempty"`
	Level    string        `yaml:"level,omitempty"`
	Aging    time.Duration `yaml:"aging,omitempty"`
	Colorful bool          `yaml:"colorful,omitempty"`
}

type Minecraft struct {
	Version *Version `yaml:"version"`
}

type Version struct {
	EntryList []*Entry `yaml:"entry-list"`
	AutoLoad  bool     `yaml:"auto-load"`
}

type Entry struct {
	Name string `yaml:"name"`
	Hash string `yaml:"hash"`
}
