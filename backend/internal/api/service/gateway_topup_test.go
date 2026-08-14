package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

// TestGatewayCreateTopup_RejectsWhenNoFxRate 资损防护：库里没有该币种的有效汇率时，
// 必须在下单前直接拒掉；否则用户支付成功后入账取不到汇率，订单永远卡 PENDING（钱收了不入账）。
func TestGatewayCreateTopup_RejectsWhenNoFxRate(t *testing.T) {
	setupGatewayServiceTest(t)

	req := GatewayCreateTopupReq{Amount: "100", Currency: "CNY", Channel: "alipay"}

	_, ec := GatewayCreateTopup(3001, req)
	if ec == nil {
		t.Fatal("expected reject when no CNY fx rate")
	}
	if ec.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("expected 400 reject, got %d (%s)", ec.HTTPStatus, ec.Msg)
	}
	// 拒单必须发生在建单之前，不留孤儿订单。
	var count int64
	store.DB.Model(&model.GatewayTopupOrder{}).Where("owner_id = ?", 3001).Count(&count)
	if count != 0 {
		t.Fatalf("expected no topup order created, got %d", count)
	}

	// 只有"未来才生效"的汇率同样要拒：不能提前用还没生效的价。
	if err := store.DB.Create(&model.GatewayFxRate{
		ID: snowflake.GenID(), FromCurrency: "CNY", ToCurrency: "USD",
		Rate: decimal.RequireFromString("0.20"), EffectiveFrom: time.Now().Add(24 * time.Hour), Source: "test-future",
	}).Error; err != nil {
		t.Fatalf("seed future fx rate: %v", err)
	}
	_, ec = GatewayCreateTopup(3001, req)
	if ec == nil || ec.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("expected 400 reject with only future-dated rate, got %+v", ec)
	}

	// 灌入已生效汇率后应通过汇率关：继续走到调支付系统下单（测试环境没有支付服务，
	// 返回内部异常而非 400 拒单，证明不再被汇率关拦住）。
	if err := store.DB.Create(&model.GatewayFxRate{
		ID: snowflake.GenID(), FromCurrency: "CNY", ToCurrency: "USD",
		Rate: decimal.RequireFromString("0.14"), EffectiveFrom: time.Now(), Source: "test",
	}).Error; err != nil {
		t.Fatalf("seed fx rate: %v", err)
	}
	_, ec = GatewayCreateTopup(3001, req)
	if ec == nil {
		t.Fatal("expected error from pay client in test env")
	}
	if ec.HTTPStatus == http.StatusBadRequest {
		t.Fatalf("fx gate should pass after seeding rate, got %+v", ec)
	}
}
