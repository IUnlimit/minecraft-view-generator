package model

type Release struct {
	// 版本号
	ID string
	// client.jar 下载地址
	ClientInfoURL string
	// client.jar 信息
	ClientInfo *ClientInfo
}

type ClientInfo struct {
	SHA1 string
	Size int64
	URL  string
}
