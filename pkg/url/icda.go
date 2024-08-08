package url

const (
	// ICDA 根路径
	ICDA = URL("https://api.loohpjames.com/spigot/plugins/interactivechatdiscordsrvaddon")
	// ICDAVersions ICDA 支持版本号列表
	ICDAVersions = ICDA + "/versions"
	// ICDAResource ICDA 资源列表
	ICDAResource = ICDA + "?minecraftVersion=%s"
)
