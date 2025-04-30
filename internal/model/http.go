package model

type common struct {
	Version string `binding:"required" json:"version"`
}

type PlayerListRequest struct {
	common
	Entry   []*PlayerListRequestEntry
	Options *PlayerListRequestOptions
}

type PlayerListRequestEntry struct {
	PlayerName string `binding:"required" json:"player-name"`
	PlayerUUID string `binding:"required" json:"player-uuid"`
	Ping       int    `binding:"required" json:"ping"`
}

type PlayerListRequestOptions struct {
	// TODO 支持关闭头像
	ShowAvatar bool `json:"show-avatar,omitempty"`
}
