package paypal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/pay/channel"
)

// fakePayPalServer 是离线可控的 PayPal API 替身：覆盖 OAuth / 下单 / 查单 / capture / 退款 /
// webhook 验签六个端点，让适配器逻辑（不含 SDK 本身）在没有真实网络的情况下被完整验证。
type fakePayPalServer struct {
	verifyStatus string // "SUCCESS" 或其它
	orderStatus  string // 非空则覆盖 GetOrder 返回的订单态且不带 capture（如 "APPROVED"=已授权未扣款）
	// captureStatus 非空则覆盖 capture 的资金态（查单与 capture 两个端点都用），订单态仍是
	// COMPLETED——这正是 PayPal 延审的真实组合："PENDING"=资金待定，钱还没到。
	captureStatus string
}

// capStatus 返回 fake 该返回的 capture 资金态，默认 COMPLETED。
func (f *fakePayPalServer) capStatus() string {
	if f.captureStatus != "" {
		return f.captureStatus
	}
	return "COMPLETED"
}

func (f *fakePayPalServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "TESTTOKEN", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/v2/checkout/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "ORDER1",
			"status": "CREATED",
			"links":  []map[string]string{{"rel": "approve", "href": "https://paypal.example/checkoutnow?token=ORDER1"}},
		})
	})
	mux.HandleFunc("/v2/checkout/orders/ORDER1", func(w http.ResponseWriter, r *http.Request) {
		if f.orderStatus != "" {
			// 覆盖：指定订单态、无 capture（模拟用户已 approve 但商户未 capture）。
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "ORDER1",
				"status": f.orderStatus,
				"purchase_units": []map[string]any{{
					"reference_id": "789",
					"amount":       map[string]string{"currency_code": "USD", "value": "9.90"},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "ORDER1",
			"status": "COMPLETED",
			"purchase_units": []map[string]any{{
				"reference_id": "789",
				"amount":       map[string]string{"currency_code": "USD", "value": "9.90"},
				"payments": map[string]any{
					"captures": []map[string]any{{
						"id":     "CAP1",
						"status": f.capStatus(),
						"amount": map[string]string{"currency_code": "USD", "value": "9.90"},
					}},
				},
			}},
		})
	})
	mux.HandleFunc("/v2/checkout/orders/ORDER1/capture", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "ORDER1",
			"status": "COMPLETED",
			"purchase_units": []map[string]any{{
				"reference_id": "789",
				"payments": map[string]any{
					"captures": []map[string]any{{
						"id":     "CAP1",
						"status": f.capStatus(),
						"amount": map[string]string{"currency_code": "USD", "value": "9.90"},
					}},
				},
			}},
		})
	})
	mux.HandleFunc("/v2/payments/captures/CAP1/refund", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "REFUND1", "status": "COMPLETED"})
	})
	mux.HandleFunc("/v1/notifications/verify-webhook-signature", func(w http.ResponseWriter, r *http.Request) {
		status := f.verifyStatus
		if status == "" {
			status = "SUCCESS"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"verification_status": status})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	cfg := Config{ClientID: "cid", Secret: "secret", WebhookID: "WH-1", APIBase: srv.URL}
	return NewAdapter(func() (Config, bool, error) { return cfg, true, nil })
}

func TestCreatePay_ReturnsApproveLink(t *testing.T) {
	srv := (&fakePayPalServer{}).start(t)
	a := testAdapter(t, srv)

	res, err := a.CreatePay(context.Background(), &channel.PayRequest{
		PayOrderID:      789,
		Amount:          decimal.NewFromFloat(9.9),
		Currency:        "USD",
		Subject:         "充值",
		ReturnURL:       "https://app.example/return",
		ReturnBridgeURL: "https://pay.example/v1/pay/return/paypal?pay_order_id=789",
	})
	require.NoError(t, err)
	assert.Equal(t, "ORDER1", res.ChannelTradeNo)
	assert.Equal(t, "https://paypal.example/checkoutnow?token=ORDER1", res.PayURL)
}

func TestCreatePay_DisabledChannel(t *testing.T) {
	a := NewAdapter(func() (Config, bool, error) { return Config{}, false, nil })
	_, err := a.CreatePay(context.Background(), &channel.PayRequest{ReturnBridgeURL: "x"})
	assert.ErrorIs(t, err, ErrDisabled)
}

func TestCompleteRedirect_CapturesSuccessfully(t *testing.T) {
	srv := (&fakePayPalServer{}).start(t)
	a := testAdapter(t, srv)

	res, err := a.CompleteRedirect(context.Background(), channel.TradeQueryRequest{PayOrderID: 789, ChannelTradeNo: "ORDER1"})
	require.NoError(t, err)
	assert.Equal(t, int64(789), res.PayOrderID)
	assert.Equal(t, channel.TradeSuccess, res.State)
	assert.True(t, decimal.NewFromFloat(9.9).Equal(res.Amount))
	assert.Equal(t, "USD", res.Currency)
}

// TestCompleteRedirect_FallbackSurvivesDeadCtx 锁死「capture 外呼把 ctx 耗尽后，兜底查单
// 仍必须跑得通」。capture 报错时适配器要查一次单才能判定钱到底扣没扣；若这次查单沿用同一个
// ctx，那么在「capture 卡满 callTimeout 才超时」这一场景下 ctx 早已烧干，查单立刻失败、兜底
// 形同虚设——而这恰恰是最需要兜底的时候（钱极可能已经扣了、只是响应没拿到）。用一个已取消的
// ctx 模拟该状态：CaptureOrder 必然失败，兜底必须靠自己派生的独立 ctx 查出 COMPLETED 并入账。
func TestCompleteRedirect_FallbackSurvivesDeadCtx(t *testing.T) {
	srv := (&fakePayPalServer{}).start(t)
	a := testAdapter(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 父 ctx 已废：模拟 capture 耗尽超时预算，或用户跳回请求中途断开

	res, err := a.CompleteRedirect(ctx, channel.TradeQueryRequest{PayOrderID: 789, ChannelTradeNo: "ORDER1"})
	require.NoError(t, err, "父 ctx 已死时兜底查单仍须确认扣款，否则钱扣了却报失败")
	assert.Equal(t, channel.TradeSuccess, res.State)
	assert.True(t, decimal.NewFromFloat(9.9).Equal(res.Amount))
	assert.Equal(t, "USD", res.Currency)
}

// TestCompleteRedirect_PendingCaptureIsNotSuccess 锁死 PayPal 延审场景不得提前入账。
// PayPal 反欺诈延审 / e-check 时 capture 会以 status=PENDING 受理——钱还没到，但此时
// 订单态照样是 COMPLETED（order.status 只表示下单流程走完）。若据订单态判成功，用户跳回
// 的当场就会入账、发货，而这笔钱最终可能被拒。必须停在 Pending，等 webhook 或对账确认。
func TestCompleteRedirect_PendingCaptureIsNotSuccess(t *testing.T) {
	srv := (&fakePayPalServer{captureStatus: "PENDING"}).start(t)
	a := testAdapter(t, srv)

	res, err := a.CompleteRedirect(context.Background(), channel.TradeQueryRequest{PayOrderID: 789, ChannelTradeNo: "ORDER1"})
	require.NoError(t, err)
	assert.Equal(t, channel.TradeSettling, res.State, "资金待定不得判成功，否则钱没到就入账")
}

// TestQueryTrade_PendingCaptureIsNotSuccess 同上，走对账查单这条路。
func TestQueryTrade_PendingCaptureIsNotSuccess(t *testing.T) {
	srv := (&fakePayPalServer{captureStatus: "PENDING"}).start(t)
	a := testAdapter(t, srv)

	st, err := a.QueryTrade(context.Background(), channel.TradeQueryRequest{PayOrderID: 789, ChannelTradeNo: "ORDER1"})
	require.NoError(t, err)
	assert.Equal(t, channel.TradeSettling, st.State, "资金待定不得判成功，否则对账会提前入账")
}

func TestQueryTrade_MapsCompletedCapture(t *testing.T) {
	srv := (&fakePayPalServer{}).start(t)
	a := testAdapter(t, srv)

	st, err := a.QueryTrade(context.Background(), channel.TradeQueryRequest{PayOrderID: 789, ChannelTradeNo: "ORDER1"})
	require.NoError(t, err)
	assert.Equal(t, channel.TradeSuccess, st.State)
	assert.True(t, decimal.NewFromFloat(9.9).Equal(st.Amount))
}

// TestQueryTrade_ApprovedIsAuthorized 覆盖"用户 approve 但商户未 capture"：查单必须
// 归一化成 TradeAuthorized（而非 TradePending），对账才能据此主动补 capture，不然
// 用户付了款却一直 CREATED、直到 PayPal 数天后自动作废。
func TestQueryTrade_ApprovedIsAuthorized(t *testing.T) {
	srv := (&fakePayPalServer{orderStatus: "APPROVED"}).start(t)
	a := testAdapter(t, srv)

	st, err := a.QueryTrade(context.Background(), channel.TradeQueryRequest{PayOrderID: 789, ChannelTradeNo: "ORDER1"})
	require.NoError(t, err)
	assert.Equal(t, channel.TradeAuthorized, st.State)
	assert.True(t, decimal.NewFromFloat(9.9).Equal(st.Amount))
	assert.Equal(t, "USD", st.Currency)
}

func TestQueryTrade_NoChannelTradeNoIsPending(t *testing.T) {
	a := NewAdapter(func() (Config, bool, error) { return Config{}, true, nil })
	st, err := a.QueryTrade(context.Background(), channel.TradeQueryRequest{PayOrderID: 1})
	require.NoError(t, err)
	assert.Equal(t, channel.TradePending, st.State)
}

func TestRefund_LooksUpCaptureAndRefunds(t *testing.T) {
	srv := (&fakePayPalServer{}).start(t)
	a := testAdapter(t, srv)

	res, err := a.Refund(context.Background(), &channel.RefundRequest{
		PayOrderID: 789, ChannelTradeNo: "ORDER1", RefundID: 999,
		Amount: decimal.NewFromFloat(9.9), Currency: "USD", Reason: "退款测试",
	})
	require.NoError(t, err)
	assert.Equal(t, channel.RefundSuccess, res.State)
	assert.Equal(t, "REFUND1", res.ChannelRefundNo)
}

// TestParseNotify_VerifiesAndParsesCaptureCompleted 覆盖审查发现的问题：resource.id
// 是 capture id，不是 order id；ChannelTradeNo 必须从 supplementary_data.related_ids.order_id
// 取，否则会把 CreateOrder 时落库的 order id 覆盖成 capture id，后续 Refund/QueryTrade
// 反查 PayPal 全部失败。
func TestParseNotify_VerifiesAndParsesCaptureCompleted(t *testing.T) {
	srv := (&fakePayPalServer{verifyStatus: "SUCCESS"}).start(t)
	a := testAdapter(t, srv)

	body := []byte(fmt.Sprintf(`{"event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"id":"CAP1","custom_id":"789","status":"COMPLETED","amount":{"currency_code":"USD","value":"9.90"},"supplementary_data":{"related_ids":{"order_id":"ORDER1"}}}}`))
	res, err := a.ParseNotify(context.Background(), http.Header{"Paypal-Transmission-Id": []string{"t1"}}, body)
	require.NoError(t, err)
	assert.Equal(t, int64(789), res.PayOrderID)
	assert.Equal(t, "ORDER1", res.ChannelTradeNo, "ChannelTradeNo 必须是 order id，不是 capture id")
	assert.Equal(t, channel.TradeSuccess, res.State)
	assert.True(t, decimal.NewFromFloat(9.9).Equal(res.Amount))
}

func TestParseNotify_MissingSupplementaryDataFallsBackToResourceID(t *testing.T) {
	srv := (&fakePayPalServer{verifyStatus: "SUCCESS"}).start(t)
	a := testAdapter(t, srv)

	body := []byte(`{"event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"id":"CAP1","custom_id":"789","amount":{"currency_code":"USD","value":"9.90"}}}`)
	res, err := a.ParseNotify(context.Background(), http.Header{}, body)
	require.NoError(t, err)
	assert.Equal(t, "CAP1", res.ChannelTradeNo, "缺 supplementary_data 时兜底用 resource.id，至少保证通知去重可用")
}

func TestParseNotify_VerificationFailedRejected(t *testing.T) {
	srv := (&fakePayPalServer{verifyStatus: "FAILURE"}).start(t)
	a := testAdapter(t, srv)

	body := []byte(`{"event_type":"PAYMENT.CAPTURE.COMPLETED","resource":{"id":"CAP1","custom_id":"789","amount":{"currency_code":"USD","value":"9.90"}}}`)
	_, err := a.ParseNotify(context.Background(), http.Header{}, body)
	assert.Error(t, err)
}

func TestParseRefundNotify_ParsesCustomIDAndInvoiceID(t *testing.T) {
	srv := (&fakePayPalServer{verifyStatus: "SUCCESS"}).start(t)
	a := testAdapter(t, srv)

	body := []byte(`{"event_type":"PAYMENT.CAPTURE.REFUNDED","resource":{"id":"REFUND1","custom_id":"789","invoice_id":"999","status":"COMPLETED","amount":{"currency_code":"USD","value":"9.90"}}}`)
	res, err := a.ParseRefundNotify(context.Background(), http.Header{}, body)
	require.NoError(t, err)
	assert.Equal(t, int64(789), res.PayOrderID)
	assert.Equal(t, int64(999), res.RefundID)
	assert.Equal(t, channel.RefundSuccess, res.State)
}

func TestSelfTest_ValidCredentialFetchesToken(t *testing.T) {
	srv := (&fakePayPalServer{}).start(t)
	err := SelfTest(context.Background(), Config{ClientID: "cid", Secret: "secret", APIBase: srv.URL})
	assert.NoError(t, err)
}

func TestSelfTest_BadEndpointFails(t *testing.T) {
	err := SelfTest(context.Background(), Config{ClientID: "cid", Secret: "secret", APIBase: "http://127.0.0.1:1"})
	assert.Error(t, err)
}
