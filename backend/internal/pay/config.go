package pay

// DefaultPort 是 pay 服务默认监听端口（27180-27189 区间内空闲位）。
const DefaultPort = 27185

// ServiceConfig 是 pay 服务的运行配置。通道商户凭证由 cmd/pay 在启动时
// 从各自的 Config（alipay.Config / paypal.Config）读取并注册，不在此聚合，
// 以保持支付核心对具体通道无感知。
type ServiceConfig struct {
	// Port HTTP 监听端口。
	Port int
	// NotifyURLBase 第三方回调可达的对外基址，如 https://pay.grix.dhf.pub。
	// 支付核心据此拼装 NotifyURL = NotifyURLBase + /v1/pay/notify/{channel}。
	NotifyURLBase string
}
