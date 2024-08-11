package url

// 1.7.2 +
// https://wiki.vg/Zh:Game_files
// https://minecraft.fandom.com/zh/wiki/Mojang_API

const (
	// VersionManifest 获取当前发布的所有 MC 版本信息
	VersionManifest = URL("https://launchermeta.mojang.com/mc/game/version_manifest.json")
	// InfoUUID 查询正版用户 UUID
	InfoUUID = URL("https://api.mojang.com/users/profiles/minecraft/%s")
	// InfoTexture 获取玩家的皮肤和披风
	InfoTexture = URL("https://sessionserver.mojang.com/session/minecraft/profile/%s")
)
