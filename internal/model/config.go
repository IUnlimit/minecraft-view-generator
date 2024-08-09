package model

import "time"

type Config struct {
	Log       *Log       `yaml:"log"`
	Minecraft *Minecraft `yaml:"minecraft"`
	Api       *Api       `yaml:"api"`
}

type Log struct {
	ForceNew bool          `yaml:"force-new,omitempty"`
	Level    string        `yaml:"level,omitempty"`
	Aging    time.Duration `yaml:"aging,omitempty"`
	Colorful bool          `yaml:"colorful,omitempty"`
}

type Minecraft struct {
	Version  *Version  `yaml:"version"`
	Resource *Resource `yaml:"resource"`
}

type Version struct {
	EntryList []*Entry `yaml:"entry-list"`
	AutoLoad  bool     `yaml:"auto-load,omitempty"`
}

type Resource struct {
	Language string `yaml:"language,omitempty"`
}

type Entry struct {
	Name string `yaml:"name,omitempty"`
	Hash string `yaml:"hash,omitempty"`
}

type Api struct {
	PlayerList *PlayerList `yaml:"player-list"`
}

type PlayerList struct {
	SingleColumnLimit int      `yaml:"single-column-limit,omitempty"`
	HeaderText        []string `yaml:"header-text,omitempty"`
	FooterText        []string `yaml:"footer-text,omitempty"`
}
