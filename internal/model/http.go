package model

type PlayerListRequest struct {
	Entry   []*PlayerListRequestEntry
	Options *PlayerListRequestOptions
}

type PlayerListRequestEntry struct {
	PlayerName string `binding:"required"`
	PlayerUUID string `binding:"required"`
	Ping       int    `binding:"required"`
}

type PlayerListRequestOptions struct {
	ShowAvatar bool
}
