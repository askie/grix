package model

import "time"

const (
	GatewayVirtualKeyStatusActive  = "active"
	GatewayVirtualKeyStatusRevoked = "revoked"
)

// GatewayVirtualKey 是发给用户的网关虚拟API Key，只存哈希，不存明文。
// 1个钱包下可开多把，互相独立、可单独吊销，但共享同一个钱包余额。
type GatewayVirtualKey struct {
	ID       int64  `gorm:"primaryKey" json:"id,string"`
	WalletID int64  `gorm:"not null;index" json:"wallet_id,string"`
	KeyHash  string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	KeyHint  string `gorm:"size:16;not null" json:"key_hint"`
	Label    string `gorm:"size:64" json:"label"`
	Status   string `gorm:"size:16;not null;default:active" json:"status"`
	// AgentID 关联"Grix中转"场景下这把Key专属服务的托管Agent（0=未关联具体Agent的通用Key，
	// 比如管理员在塘主后台代发的那种）。虚拟Key明文只在创建那一刻能拿到，之后系统只存哈希，
	// 所以"配置某个Agent用Grix中转"这个动作靠这个字段判断是否已经配过、避免重复发Key。
	AgentID int64 `gorm:"not null;default:0;index" json:"agent_id,string"`
	// RelayModel 是"Grix中转"启用时为该Agent选定的服务端模型（原生配置类型的CLI会用
	// 这个名字发请求；空=未指定，走网关模型映射/兜底）。随每次签发写入、随Key生命周期存续，
	// 桌面端"大模型设置"的Agent中转列表靠它回显上次选中的模型。
	RelayModel string     `gorm:"size:128;not null;default:''" json:"relay_model"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

func (GatewayVirtualKey) TableName() string { return "gateway_virtual_keys" }
