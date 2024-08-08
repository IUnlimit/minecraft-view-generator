package url

import "fmt"

type URL string

// Format 获取格式化后的 URL
func (u URL) Format(args ...any) string {
	if len(args) == 0 {
		return string(u)
	}
	return fmt.Sprintf(string(u), args...)
}
