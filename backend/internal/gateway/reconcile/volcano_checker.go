package reconcile

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/volcengine/volcengine-go-sdk/service/billing"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

// VolcanoArkBalanceChecker 查火山引擎账户余额（费用中心 QueryBalanceAcct 管控面API）。
//
// 火山方舟推理用的 ARK Key（Bearer token）本身查不到余额（实测常见路径均404）；余额只能走
// 账户级别的费用中心API，需要另一套 AK/SK（访问密钥对）签名认证，用官方 volcengine-go-sdk
// 处理签名，不自己实现签名算法。AvailableBalance 字段名从SDK生成代码核对，已用真实 AK/SK 跑通、
// 真实查到过账户余额。
//
// 注意：这个余额是账户整体余额。如果这个火山账户除了Ark还跑别的火山云服务，余额变化就不只
// 反映Ark的消费了——现在假设这个账户是专门给Ark/豆包用的（跟DeepSeek那个专用测试账户一样）。
type VolcanoArkBalanceChecker struct {
	client *billing.BILLING
}

func NewVolcanoArkBalanceChecker(ak, sk, region string) (*VolcanoArkBalanceChecker, error) {
	if region == "" {
		region = "cn-beijing"
	}
	cfg := volcengine.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(ak, sk, "")).
		WithRegion(region)
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("volcengine session: %w", err)
	}
	return &VolcanoArkBalanceChecker{client: billing.New(sess)}, nil
}

// CurrentBalanceCNY 返回火山引擎账户当前的人民币可用余额。
func (c *VolcanoArkBalanceChecker) CurrentBalanceCNY(ctx context.Context) (decimal.Decimal, error) {
	output, err := c.client.QueryBalanceAcctWithContext(volcengine.Context(ctx), &billing.QueryBalanceAcctInput{})
	if err != nil {
		return decimal.Zero, fmt.Errorf("query volcengine balance: %w", err)
	}
	if output == nil || output.AvailableBalance == nil {
		return decimal.Zero, fmt.Errorf("query volcengine balance: response missing AvailableBalance, raw=%+v", output)
	}
	return decimal.NewFromString(*output.AvailableBalance)
}
