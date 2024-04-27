package url

import "fmt"

// 1.7.2 +
// https://wiki.vg/Zh:Game_files
// https://minecraft.fandom.com/zh/wiki/Mojang_API

type URL string

const (
	// VersionManifest 获取当前发布的所有 MC 版本信息
	VersionManifest = URL("https://launchermeta.mojang.com/mc/game/version_manifest.json")
	// InfoUUID 查询正版用户 UUID
	InfoUUID = URL("https://api.mojang.com/users/profiles/minecraft/%s")
)

// Format 获取格式化后的 URL
func (u URL) Format(args ...string) string {
	if len(args) == 0 {
		return string(u)
	}
	return fmt.Sprintf(string(u), args)
}
