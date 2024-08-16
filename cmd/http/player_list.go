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
// @Description 获取在线玩家列表的图像. 如果传入玩家为空，则返回错误码
// @Tags v1
// @Accept json
// @Produce json
// @Param playerListRequest body model.PlayerListRequest true "玩家列表请求数据"
// @Success 200 {object} SuccessResponse "成功响应"
// @Failure 400 {object} ErrorResponse "失败响应"
// @Router /get_player_list [post]
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
