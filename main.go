package main

import (
	"github.com/IUnlimit/minecraft-view-generator/cmd/generator"
	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/loader"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"github.com/IUnlimit/minecraft-view-generator/pkg/sdl"
)

func main() {
	conf.Init()
	logger.Init()
	loader.Init()
	go generator.Serve()
	sdl.Init()
}
