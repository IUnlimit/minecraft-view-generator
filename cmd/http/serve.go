package http

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/IUnlimit/minecraft-view-generator/docs"
	global "github.com/IUnlimit/minecraft-view-generator/internal"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	swaggerfiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware
)

func Serve(indexPage []byte) {
	port := 4399

	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.MultiWriter(os.Stdout, logger.Hook.GetWriter())

	engine := gin.Default()
	engine.Use(gin.Recovery())

	engine.GET("", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/api/v1/swagger/index.html")
	})

	engine.GET(global.Skinview3dUri, func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexPage)
	})
	engine.StaticFS(global.Skinview3dUri+"/fonts", gin.Dir(global.FontsPath, false))
	engine.StaticFS(global.Skinview3dUri+"/skins", gin.Dir(global.SkinsPath, false))

	docs.SwaggerInfo.BasePath = "/api/v1"
	v1 := engine.Group(docs.SwaggerInfo.BasePath)
	v1.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))

	v1.GET(global.PingUri, Ping)
	v1.POST(global.GetPlayerListUri, GetPlayerList)

	log.Infof("Swagger will start on http://127.0.0.1:%d", port)
	log.Infof("Skinview3d will start on http://127.0.0.1:%d/skinview3d", port)
	err := engine.Run(fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Http server occurred error, %v", err)
	}
}
