package http

import (
	"fmt"
	"io"
	"net/http"
	"os"

	docs "github.com/IUnlimit/minecraft-view-generator/docs"
	"github.com/IUnlimit/minecraft-view-generator/internal/logger"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	swaggerfiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware
)

func Serve() {
	port := 4399

	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.MultiWriter(os.Stdout, logger.Hook.GetWriter())

	engine := gin.Default()
	engine.Use(gin.Recovery())

	engine.GET("", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/api/v1/swagger/index.html")
	})

	docs.SwaggerInfo.BasePath = "/api/v1"
	v1 := engine.Group(docs.SwaggerInfo.BasePath)
	v1.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))

	v1.GET("/ping", Ping)
	v1.POST("/get_player_list", GetPlayerList)

	log.Infof("Http server will start on port %d", port)
	err := engine.Run(fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Http server occurred error, %v", err)
	}
}
