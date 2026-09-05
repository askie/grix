package liveactivity

import "fmt"

// 活动 token 的存放约定。写在这里而不是各自的包里：api 服务写、push 服务读，
// 两边必须对同一个 key 和同一种 value 形状达成一致。
//
// 只在 Redis 里存、不建表：一张卡片的 token 最多活一天，下一次 run 会换一批，
// 而且卡片本身就是易失的（用户划掉、系统回收都不会通知后端）。

// TokenTTLSeconds 是活动 token 的存活时长（秒）。
const TokenTTLSeconds = 24 * 60 * 60

// TokenKey 是某主人某会话下全部活动 token 的 hash key。
// hash 的 field 是 device_id：同一个会话可能在手机和 iPad 上各开一张卡。
func TokenKey(userID int64, sessionID string) string {
	return fmt.Sprintf("im:la:tokens:%d:%s", userID, sessionID)
}

// TokenEntry 是 hash 里每台设备存的值。
type TokenEntry struct {
	ActivityID string `json:"activity_id"`
	Token      string `json:"token"`
}
