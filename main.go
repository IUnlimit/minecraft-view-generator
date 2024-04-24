package main

import (
	"github.com/IUnlimit/minecraft-view-generator/cmd/generator"
	"github.com/IUnlimit/minecraft-view-generator/internal/conf"
	"github.com/IUnlimit/minecraft-view-generator/internal/loader"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
)

func main() {
	conf.Init()
	logger.Init()
	loader.Init()
	generator.Serve()
}
