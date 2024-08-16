package http

import (
	"github.com/IUnlimit/minecraft-view-generator/internal/handler"
	"github.com/IUnlimit/minecraft-view-generator/internal/model"
	"github.com/IUnlimit/minecraft-view-generator/tools"
	"github.com/gin-gonic/gin"
)

// @BasePath /api/v1

// GetPlayerList godoc
// @Summary 获取在线玩家列表的图像
// @Schemes
// @Description 描述xxx
// @Tags image, player_list
// @Accept json
// @Produce json
// @Success 200 {string} Helloworld
// @Router /get_player_list [get]
func GetPlayerList(ctx *gin.Context) {
	var request model.PlayerListRequest
	if err := ctx.ShouldBind(&request); err != nil {
		ResponseError(err, ctx)
		return
	}

	image, err := handler.GetPlayerList(&request)
	if err != nil {
		ResponseError(err, ctx)
		return
	}

	base64, err := tools.Image2Base64(image)
	if err != nil {
		ResponseError(err, ctx)
		return
	}

	ResponseSuccess(base64, ctx)
}
