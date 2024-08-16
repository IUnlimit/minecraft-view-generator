package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kylelemons/godebug/pretty"
	log "github.com/sirupsen/logrus"
)

type SuccessResponse struct {
	Code uint `json:"code"`
	Data any  `json:"data"`
}

type ErrorResponse struct {
	Code    uint   `json:"code"`
	Message string `json:"message"`
}

func Ping(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "pong",
	})
}

func ResponseSuccess(data any, ctx *gin.Context) {
	ctx.JSON(http.StatusOK, &SuccessResponse{
		Code: http.StatusOK,
		Data: data,
	})
}

func ResponseError(err error, ctx *gin.Context) {
	ctx.JSON(http.StatusBadRequest, &ErrorResponse{
		Code:    http.StatusBadRequest,
		Message: err.Error(),
	})
	log.Errorf("Http request(%s) response error, %v", pretty.Sprint(ctx.Request), err)
}
