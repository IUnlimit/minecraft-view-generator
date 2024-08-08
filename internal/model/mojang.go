package model

type Release struct {
	// 版本号
	ID string
	// package 信息
	PackageInfoURL string
	// client.jar 信息
	ClientInfo *ClientInfo
}

type ClientInfo struct {
	SHA1 string
	Size int64
	Url  string
}
