package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kylelemons/godebug/pretty"
	log "github.com/sirupsen/logrus"
)

func Ping(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "pong",
	})
}

func ResponseSuccess(data any, ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": data,
	})
}

func ResponseError(err error, ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"code":    http.StatusBadRequest,
		"message": err.Error(),
	})
	log.Errorf("Http request(%s) response error, %v", pretty.Sprint(ctx.Request), err)
}
