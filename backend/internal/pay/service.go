package pay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pay/channel"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
)

// 支付核心错误。
var (
	ErrMissingField        = errors.New("pay: missing required field")
	ErrInvalidAmount       = errors.New("pay: amount must be positive")
	ErrChannelNotFound     = errors.New("pay: channel not registered")
	ErrCurrencyUnsupported = errors.New("pay: currency not supported by channel")
	ErrOrderNotFound       = errors.New("pay: order not found")
	ErrOrderNotRefundable  = errors.New("pay: order not in a refundable state")
	ErrRefundExceed        = errors.New("pay: refund amount exceeds refundable balance")
	ErrNotifyMismatch      = errors.New("pay: notify channel/amount/currency mismatch with order")
)

// staleOrderTTL 是「用户压根没付」的支付单在被对账关掉之前的存活期。
//
// 取 6h 是为了同时满足两个约束：
//
//	① 必须晚于第三方未支付订单的自然失效时间——PayPal 未 approve 的 Order 大约 3h 过期，
//	   过了这个点用户已不可能再付款，关单不会误杀真实付款。6h 留了一倍余量。
//	② 又必须足够短，让废单尽快退出 CREATED 集合。废单是支付漏斗里的常态（开了收银台就走），
//	   它们滞留在 CREATED 期间会占用对账扫描的 LIMIT 名额；滞留窗口内的废单一旦超过 LIMIT，
//	   真正需要补偿的「已授权没跳回来」的单子就得排队等，而 PayPal 那张 order 等不了太久
//	   （见 reconcileScanLimit 的说明）。窗口从 24h 收到 6h，滞留量直接降到四分之一。
//
// 只作用于 TradePending（用户没付）。已授权的走 TradeAuthorized 补 capture，资金待定的
// 走 TradeSettling 原地等待，两者都不会被关。
const staleOrderTTL = 6 * time.Hour

// ReconcileScanLimit 是对账每轮扫描的单数上限，供 cmd/pay 的对账循环使用。
//
// 这个值必须显著大于「滞留窗口内的 CREATED 单数」，否则会饿死：扫描按 created_at 升序取
// 前 N 条，滞留的废单一旦占满 N，新产生的「已授权没跳回来」的单子就永远排不进来——而这
// 类单只能靠对账补偿（PayPal 不会为从未发生的 capture 发 webhook），排不进来就是漏入账，
// 而且毫无报错。
//
// 取 1000（配合 6h 的 staleOrderTTL）：要撑爆它，需要 6 小时内产生 1000 张以上的废单。
// 这给了当前业务量很大的余量。但它终究只是把阈值抬高，没有根治——扫描没有轮转游标，永远
// 只看最老的那一段。根治要把 ReconcileStale 改成 keyset 游标翻页。
const ReconcileScanLimit = 1000

// reconcileFailureSamples 是对账失败汇总告警里附带的原因样本条数。够定位问题就行——
// 全军覆没时前几条和后几百条的原因是一样的（凭证配错 / 通道不可用），带全量只会撑爆事件体。
const reconcileFailureSamples = 3

// Service 是支付核心：编排状态机、三层幂等、通道路由与对外事件。
type Service struct {
	store            *Store
	registry         *channel.Registry
	notifier         Notifier
	notifyURLBase    string          // 回调基址，如 https://pay.grix.dhf.pub
	returnAllowHosts map[string]bool // 允许 302 跳回的 return_url 主机白名单（防开放重定向）
	newID            func() int64    // ID 生成器，默认 snowflake，可在测试注入确定性实现
}

// NewService 组装支付核心。return_url 跳回白名单从 notifyURLBase 的主机派生：前端与
// 支付回调同域（CN=grix.dhf.pub / 全球=gb.grix.im），故落到该域名的 return_url 放行，
// 其余外域一律不跳（退化为纯文本完成页），堵住开放重定向。
func NewService(store *Store, registry *channel.Registry, notifier Notifier, notifyURLBase string) *Service {
	if notifier == nil {
		notifier = NopNotifier{}
	}
	allow := make(map[string]bool)
	if u, err := url.Parse(notifyURLBase); err == nil && u.Host != "" {
		allow[strings.ToLower(u.Host)] = true
	}
	return &Service{store: store, registry: registry, notifier: notifier, notifyURLBase: notifyURLBase, returnAllowHosts: allow, newID: snowflake.GenID}
}

// ReturnURLAllowed 判定收银台跳回地址是否可安全 302 过去，防开放重定向被拿去钓鱼。
// 相对路径（无 scheme 无 host）天然同源，放行；带主机的绝对地址必须命中白名单。
// 空串按调用方约定另作处理（不跳转），这里返回 false。
func (s *Service) ReturnURLAllowed(returnURL string) bool {
	if returnURL == "" {
		return false
	}
	// 浏览器收到 302 Location 后会把反斜杠归一成正斜杠再解析，故这里先做同样归一
	// 再交给 url.Parse。否则 `/\evil.com` `\\evil.com` 等会被 url.Parse 判成相对
	// 路径（Host 为空）而放行，浏览器却按 `//evil.com`（协议相对）跳到外域——开放
	// 重定向绕过。归一后这类输入 Host 解析成 evil.com，自然被白名单挡掉。
	normalized := strings.ReplaceAll(returnURL, "\\", "/")
	u, err := url.Parse(normalized)
	if err != nil {
		return false
	}
	if u.Host == "" && u.Scheme == "" {
		// 同源相对路径才放行：必须单个 "/" 开头且不以 "//" 开头。否则 "////evil.com"
		// 这类空主机的网络路径引用（url.Parse 解出的 Host 也为空）会被浏览器当作
		// 协议相对地址跳到外域，仅靠反斜杠归一挡不住。
		return strings.HasPrefix(normalized, "/") && !strings.HasPrefix(normalized, "//")
	}
	// 主机名大小写不敏感（RFC 3986），统一小写再比白名单，避免把 GRIX.DHF.PUB 这类
	// 合法跳回地址误拒、让用户付完钱跳不回完成页。
	return s.returnAllowHosts[strings.ToLower(u.Host)]
}

// CreateOrderRequest 是业务侧的下单入参。
type CreateOrderRequest struct {
	BizType    string
	BizOrderID string
	Channel    string
	Amount     decimal.Decimal
	Currency   string
	Subject    string
	ReturnURL  string
	Extra      map[string]string
}

// CreateOrderResult 是下单返回，PayURL 供业务拉起收银台。
type CreateOrderResult struct {
	PayOrderID     int64
	Status         string
	PayURL         string
	ChannelTradeNo string
}

// CreateOrder 创建支付单并向通道下单，返回收银台跳转。第一层幂等：
// 同一 biz_type+biz_order_id 只会有一张支付单。
func (s *Service) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResult, error) {
	if req.BizType == "" || req.BizOrderID == "" || req.Channel == "" || req.Currency == "" {
		return nil, ErrMissingField
	}
	if !req.Amount.IsPositive() {
		return nil, ErrInvalidAmount
	}
	ch, ok := s.registry.Lookup(req.Channel)
	if !ok {
		return nil, ErrChannelNotFound
	}
	if !s.registry.SupportsCurrency(req.Channel, req.Currency) {
		return nil, ErrCurrencyUnsupported
	}

	order := &model.PayOrder{
		ID:         s.newID(),
		BizType:    req.BizType,
		BizOrderID: req.BizOrderID,
		Channel:    req.Channel,
		Amount:     req.Amount,
		Currency:   req.Currency,
		Status:     model.PayOrderStatusCreated,
		Subject:    req.Subject,
	}
	saved, existed, err := s.store.CreateOrder(order)
	if err != nil {
		return nil, err
	}

	res := &CreateOrderResult{PayOrderID: saved.ID, Status: saved.Status, ChannelTradeNo: saved.ChannelTradeNo}
	if existed && saved.Status != model.PayOrderStatusCreated {
		// 已支付 / 已关单等：幂等返回当前状态，不再拉收银台。
		return res, nil
	}

	pr, err := ch.CreatePay(ctx, &channel.PayRequest{
		PayOrderID:      saved.ID,
		Amount:          saved.Amount,
		Currency:        saved.Currency,
		Subject:         saved.Subject,
		NotifyURL:       s.notifyURL(saved.Channel),
		ReturnURL:       req.ReturnURL,
		ReturnBridgeURL: s.returnBridgeURL(saved.Channel, saved.ID, req.ReturnURL),
		Extra:           req.Extra,
	})
	if err != nil {
		if !existed {
			// 全新单下单失败，标记失败，避免留下无法支付的悬挂单。标记本身再失败就告警：
			// 这单会以 CREATED 悬在库里（对账会兜住、超龄关掉，但状态一度是脏的）。
			if _, merr := s.store.MarkFailed(saved.ID); merr != nil {
				s.publish(EventOrderStateInconsistent, saved.ID,
					s.inconsistentEvent(saved, fmt.Sprintf("向通道下单失败后标记订单失败也失败了，订单将悬挂在 CREATED: %v", merr)))
			}
		}
		return nil, err
	}
	res.PayURL = pr.PayURL
	if pr.ChannelTradeNo != "" {
		res.ChannelTradeNo = pr.ChannelTradeNo
		// 部分通道（PayPal）下单即拿到交易号，必须立即落库：用户跳转确认 /
		// 对账查单 / 退款都要靠它反查第三方，等不到 MarkPaid 才写入。
		if err := s.store.BindChannelTradeNo(saved.ID, pr.ChannelTradeNo); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// HandleNotify 处理第三方入站支付通知：验签 → 记流水 → 校验 → 幂等推进 PAID → 发事件。
// 返回 nil 表示可向第三方回 success ACK。
func (s *Service) HandleNotify(ctx context.Context, channelCode string, headers http.Header, raw []byte) error {
	ch, ok := s.registry.Lookup(channelCode)
	if !ok {
		return ErrChannelNotFound
	}
	res, err := ch.ParseNotify(ctx, headers, raw)
	if err != nil {
		return err // 验签失败等
	}
	order, err := s.store.GetOrder(res.PayOrderID)
	if err != nil {
		return ErrOrderNotFound
	}
	_, err = s.applyNotifyResult(order, channelCode, string(raw), res)
	return err
}

// CompleteRedirect 处理用户从收银台跳转回来的确认：若通道需要二次确认（如 PayPal
// capture），调用通道完成确认并按与异步通知相同的幂等路径入账；不需要二次确认的
// 通道（如支付宝）直接返回当前单据状态，实际入账仍以异步通知/对账为准。
func (s *Service) CompleteRedirect(ctx context.Context, payOrderID int64) (*model.PayOrder, error) {
	order, err := s.store.GetOrder(payOrderID)
	if err != nil {
		return nil, ErrOrderNotFound
	}
	if order.Status != model.PayOrderStatusCreated {
		// 已经是终态/已入账（多半是 webhook 先一步落地）：不再重复调用通道二次确认，
		// 直接返回当前单据。避免拿一个可能已经变化的 ChannelTradeNo 去发起一次注定
		// 失败的确认请求，把已经成功的支付误报成 "failed" 给用户看。
		return order, nil
	}
	ch, ok := s.registry.Lookup(order.Channel)
	if !ok {
		return nil, ErrChannelNotFound
	}
	rc, ok := ch.(channel.RedirectCapturer)
	if !ok {
		return order, nil
	}
	res, err := rc.CompleteRedirect(ctx, channel.TradeQueryRequest{PayOrderID: payOrderID, ChannelTradeNo: order.ChannelTradeNo})
	if err != nil {
		return nil, err
	}
	if _, err := s.applyNotifyResult(order, order.Channel, res.Raw, res); err != nil {
		return nil, err
	}
	return s.store.GetOrder(payOrderID)
}

// applyNotifyResult 是支付结果落地的唯一入口：无论来自异步通知还是跳转返回后的
// 主动确认，都在这里做第二层幂等（通知流水去重）+ 安全校验（通道/金额/币种）+
// 状态机推进 + 发事件。raw 仅用于流水存档，可以是原始报文也可以是确认结果的 JSON。
// applyNotifyResult 返回 advanced=true 表示这次真的推进了订单状态（重复通知会是 false）。
func (s *Service) applyNotifyResult(order *model.PayOrder, channelCode string, raw string, res *channel.NotifyResult) (bool, error) {
	if _, err := s.store.NotifySeen(s.newID(), channelCode, res.ChannelTradeNo, raw); err != nil {
		return false, err
	}
	// 安全校验：通道、金额、币种必须与原单一致，否则拒绝入账并上抛（告警）。
	if order.Channel != channelCode || !order.Amount.Equal(res.Amount) || order.Currency != res.Currency {
		return false, ErrNotifyMismatch
	}
	switch res.State {
	case channel.TradeSuccess:
		return s.finalizePaid(order, res.ChannelTradeNo)
	case channel.TradeClosed:
		return s.finalizeClosed(order, res.ChannelTradeNo)
	case channel.TradeFailed:
		return s.store.MarkFailed(order.ID)
	}
	return false, nil
}

// finalizePaid 幂等推进 CREATED → PAID，并在真正推进时发成功事件。
// 返回 advanced=true 表示这次调用真的把订单推进了（重复入账会是 false）。
func (s *Service) finalizePaid(order *model.PayOrder, channelTradeNo string) (bool, error) {
	advanced, err := s.store.MarkPaid(order.ID, channelTradeNo, time.Now())
	if err != nil {
		return false, err
	}
	if advanced {
		s.publish(EventOrderPaid, order.ID, s.orderEvent(order, model.PayOrderStatusPaid, channelTradeNo))
	}
	return advanced, nil
}

// RefundRequest 是业务侧发起退款的入参。
type RefundRequest struct {
	PayOrderID  int64
	BizRefundID string
	Amount      decimal.Decimal
	Reason      string
}

// CreateRefund 发起一笔退款。第三层幂等：同一 biz_refund_id 只退一次。
func (s *Service) CreateRefund(ctx context.Context, req RefundRequest) (*model.PayRefund, error) {
	if req.BizRefundID == "" || req.PayOrderID == 0 {
		return nil, ErrMissingField
	}
	if !req.Amount.IsPositive() {
		return nil, ErrInvalidAmount
	}
	// 幂等优先：同一 biz_refund_id 已存在则直接返回，不受支付单当前状态影响。
	if dup, err := s.store.GetRefundByBiz(req.BizRefundID); err != nil {
		return nil, err
	} else if dup != nil {
		return dup, nil
	}
	order, err := s.store.GetOrder(req.PayOrderID)
	if err != nil {
		return nil, ErrOrderNotFound
	}
	if order.Status != model.PayOrderStatusPaid && order.Status != model.PayOrderStatusPartialRefunded {
		return nil, ErrOrderNotRefundable
	}
	ch, ok := s.registry.Lookup(order.Channel)
	if !ok {
		return nil, ErrChannelNotFound
	}
	// 可退余额校验：已退（含退款中）+ 本次 ≤ 支付金额。
	sumBefore, err := s.store.SumRefunded(order.ID)
	if err != nil {
		return nil, err
	}
	if sumBefore.Add(req.Amount).GreaterThan(order.Amount) {
		return nil, ErrRefundExceed
	}

	refund := &model.PayRefund{
		ID:          s.newID(),
		PayOrderID:  order.ID,
		BizRefundID: req.BizRefundID,
		Amount:      req.Amount,
		Currency:    order.Currency,
		Status:      model.PayRefundStatusRefunding,
	}
	saved, existed, err := s.store.CreateRefund(refund)
	if err != nil {
		return nil, err
	}
	if existed {
		return saved, nil // 幂等返回
	}
	if err := s.store.UpdateOrderRefundStatus(order.ID, model.PayOrderStatusRefunding); err != nil {
		return nil, err
	}

	rr, err := ch.Refund(ctx, &channel.RefundRequest{
		PayOrderID:     order.ID,
		ChannelTradeNo: order.ChannelTradeNo,
		RefundID:       saved.ID,
		Amount:         req.Amount,
		TotalAmount:    order.Amount,
		Currency:       order.Currency,
		Reason:         req.Reason,
	})
	if err != nil {
		return nil, err
	}
	s.applyRefundState(order, saved, sumBefore, rr.State, rr.ChannelRefundNo)
	return s.store.GetRefund(saved.ID)
}

// HandleRefundNotify 处理第三方异步退款结果通知。
func (s *Service) HandleRefundNotify(ctx context.Context, channelCode string, headers http.Header, raw []byte) error {
	ch, ok := s.registry.Lookup(channelCode)
	if !ok {
		return ErrChannelNotFound
	}
	res, err := ch.ParseRefundNotify(ctx, headers, raw)
	if err != nil {
		return err
	}
	refund, err := s.store.GetRefund(res.RefundID)
	if err != nil {
		return err
	}
	order, err := s.store.GetOrder(res.PayOrderID)
	if err != nil {
		return ErrOrderNotFound
	}
	sumBefore, err := s.store.SumRefunded(order.ID)
	if err != nil {
		return err
	}
	// sumBefore 含本退款单（REFUNDING 状态），回算时扣除本单以还原"本单之前"的已退额。
	sumBefore = sumBefore.Sub(refund.Amount)
	switch res.State {
	case channel.RefundSuccess:
		s.applyRefundState(order, refund, sumBefore, channel.RefundSuccess, res.ChannelRefundNo)
	case channel.RefundFailed:
		s.applyRefundState(order, refund, sumBefore, channel.RefundFailed, res.ChannelRefundNo)
	}
	return nil
}

// applyRefundState 依据通道退款结果推进退款单与支付单状态，并发对应事件。
// sumBefore 为「本退款单之前」已退（含退款中）金额。
func (s *Service) applyRefundState(order *model.PayOrder, refund *model.PayRefund, sumBefore decimal.Decimal, state channel.RefundState, channelRefundNo string) {
	switch state {
	case channel.RefundSuccess:
		// 仅当退款单确实从 REFUNDING 推进到 REFUNDED 时，才动支付单 + 发事件。
		// 重复 / 乱序通知在这里被挡下（advanced=false），不重复发事件、不破坏状态机。
		advanced, err := s.store.UpdateRefundStatus(refund.ID, model.PayRefundStatusRefunded, channelRefundNo)
		if err != nil || !advanced {
			return
		}
		orderStatus := model.PayOrderStatusPartialRefunded
		if sumBefore.Add(refund.Amount).GreaterThanOrEqual(order.Amount) {
			orderStatus = model.PayOrderStatusRefunded
		}
		// 钱已经退出去了，订单的退款状态却没更新上——不能吞。可退余额靠退款单求和算，
		// 不会因此重复退款，但订单状态是脏的，必须告警让人来对。
		if err := s.store.UpdateOrderRefundStatus(order.ID, orderStatus); err != nil {
			s.publish(EventOrderStateInconsistent, order.ID,
				s.inconsistentEvent(order, fmt.Sprintf("退款已成功但订单退款状态更新失败（应为 %s）: %v", orderStatus, err)))
		}
		s.publish(EventRefundSucceeded, order.ID, s.refundEvent(order, refund, model.PayRefundStatusRefunded))
	case channel.RefundFailed:
		advanced, err := s.store.UpdateRefundStatus(refund.ID, model.PayRefundStatusFailed, channelRefundNo)
		if err != nil || !advanced {
			return
		}
		// 退款失败：无历史已退则回到 PAID，否则维持部分退款。
		restore := model.PayOrderStatusPaid
		if sumBefore.IsPositive() {
			restore = model.PayOrderStatusPartialRefunded
		}
		// 回滚订单状态失败：订单会卡在 REFUNDING，再也退不了款（CreateRefund 要求可退状态）。
		// 必须告警。
		if err := s.store.UpdateOrderRefundStatus(order.ID, restore); err != nil {
			s.publish(EventOrderStateInconsistent, order.ID,
				s.inconsistentEvent(order, fmt.Sprintf("退款失败后订单状态回滚失败（应恢复为 %s，否则将卡在 REFUNDING 无法再退）: %v", restore, err)))
		}
		s.publish(EventRefundFailed, order.ID, s.refundEvent(order, refund, model.PayRefundStatusFailed))
	case channel.RefundProcessing:
		// 异步退款受理中：保持 REFUNDING，等 HandleRefundNotify。
	}
}

// ReconcileOne 对单张 CREATED 支付单主动查单补偿：若第三方已成功则推进 PAID。
//
// 返回 advanced=true 表示这一轮**真的推进了订单状态**（入账或关单）；false 表示查完什么都
// 没变（用户还没付、资金还在路上等），这是正常的 no-op，不是失败。二者必须分开——否则
// cmd/pay 的日志会把一堆 no-op 算成"推进了 N 单"，对账全面停摆时运维看日志还以为一切正常。
func (s *Service) ReconcileOne(ctx context.Context, order *model.PayOrder) (bool, error) {
	ch, ok := s.registry.Lookup(order.Channel)
	if !ok {
		return false, ErrChannelNotFound
	}
	st, err := ch.QueryTrade(ctx, channel.TradeQueryRequest{PayOrderID: order.ID, ChannelTradeNo: order.ChannelTradeNo})
	if err != nil {
		return false, err
	}
	switch st.State {
	case channel.TradeSuccess:
		if !order.Amount.Equal(st.Amount) || order.Currency != st.Currency {
			return false, ErrNotifyMismatch
		}
		return s.finalizePaid(order, st.ChannelTradeNo)
	case channel.TradeAuthorized:
		// 用户已在第三方授权但未扣款（PayPal approve 后没跳回来，capture 从未发生）：
		// 主动补一次二次确认。不补的话订单会一直 CREATED，直到 PayPal 数天后自动作废，
		// 用户付了款却既不到账也无失败提示。走与用户跳回同一条 CompleteRedirect + 幂等
		// 入账路径；通道不支持二次确认（理论上不会返回此态）则跳过等自然过期。
		rc, ok := ch.(channel.RedirectCapturer)
		if !ok {
			return false, nil
		}
		res, err := rc.CompleteRedirect(ctx, channel.TradeQueryRequest{PayOrderID: order.ID, ChannelTradeNo: order.ChannelTradeNo})
		if err != nil {
			return false, err
		}
		return s.applyNotifyResult(order, order.Channel, res.Raw, res)
	case channel.TradeSettling:
		// 资金待定（延审 / e-check）：用户确实付了、第三方也受理了，只是钱在路上。什么都
		// 不做——既不入账（钱还没到），更不能当废单关掉（关了这笔到账的钱就丢了）。等
		// capture 转 COMPLETED 的 webhook，或下一轮对账查到 Success。
		return false, nil
	case channel.TradePending:
		// 用户压根没付（收银台开了又跑）。这类废单在支付漏斗里是常态，若放任不管会永远
		// 停在 CREATED 无限累积——而对账扫描有 LIMIT，废单堆过阈值后每轮就只捞得到这批
		// 最老的死单，真正需要补偿的「已授权没跳回来」的单子再也扫不到，第 1 项的补偿
		// 会静默失效。所以超龄的废单必须关掉，让它退出 CREATED 集合。
		//
		// 关单是安全的：用户一旦授权，查单返回的是 TradeAuthorized（走上面补 capture），
		// 落到这里就说明第三方此刻确实没有任何扣款；且 staleOrderTTL 远晚于 PayPal 未
		// approve 订单的自然失效时间，此时用户已不可能再付款。资金待定的单子走上面的
		// TradeSettling 分支，永不经过这里。
		if time.Since(order.CreatedAt) < staleOrderTTL {
			return false, nil // 还在有效期内，用户可能回来付，下轮再看
		}
		return s.finalizeClosed(order, st.ChannelTradeNo)
	case channel.TradeFailed:
		// 第三方明确拒付（延审被拒 / 扣款失败）。DENIED 是终态，不会再翻盘。
		//
		// 必须在这里落终态，不能指望 webhook：一旦 PAYMENT.CAPTURE.DENIED 那条通知丢了，
		// 这单就永远卡在 CREATED——而且它躲得开上面的废单收尸（TTL 关单只在 TradePending
		// 分支里，被拒单归的是 TradeFailed，永远走不到那儿），于是成为一个永不退出扫描集合
		// 的永久占位符，每轮白查一次第三方、占一个 LIMIT 名额，直到天荒地老。
		//
		// 安全：TradeFailed 意味着第三方明确报告钱没扣成，MarkFailed 的 CAS 又只从 CREATED
		// 推进，不会误伤任何已入账的单。
		return s.store.MarkFailed(order.ID)
	case channel.TradeClosed:
		return s.finalizeClosed(order, st.ChannelTradeNo)
	}
	return false, nil
}

// finalizeClosed 幂等关单，并在真正推进时发 EventOrderClosed。
//
// 关单必须发事件：业务方（如网关充值单）靠支付事件驱动自己的状态机，只发 paid 不发 closed
// 的话，被关掉的支付单对应的充值单会永远停在 PENDING——支付侧 CLOSED、业务侧还挂着，数据
// 对不上。以前废单不关时这个缺口不明显，现在废单会被批量关掉，必须补上。
func (s *Service) finalizeClosed(order *model.PayOrder, channelTradeNo string) (bool, error) {
	advanced, err := s.store.MarkClosed(order.ID)
	if err != nil {
		return false, err
	}
	if advanced {
		s.publish(EventOrderClosed, order.ID, s.orderEvent(order, model.PayOrderStatusClosed, channelTradeNo))
	}
	return advanced, nil
}

// ReconcileStale 扫描创建早于 before、仍 CREATED 的支付单，逐张主动查单补偿。
// 兜住第三方异步通知丢失的情况（审查 R1）。
//
// 返回 advanced=本轮**真的推进了状态**的单数（入账 / 关单），failed=查单失败的单数。查完
// 没变化的 no-op（用户还没付、资金还在路上）两个都不计——它们既不是成绩也不是故障。
// advanced 必须只统计真推进：否则对账全面停摆时，日志会把一屋子 no-op 报成"推进了 N 单"，
// 运维看着还以为一切正常。
//
// 单张失败不中断整轮（一张单查不动不该拖垮其余的），但**绝不静默吞掉**：对账是防丢钱的
// 最后一道防线，它自己失效必须能被看见。凭证配错、PayPal 长时间不可用这类问题会让每一
// 张单都失败，若只是闷头 continue，整条补偿链路瘫痪而日志里一片安静——和第 8 项那种
// "静默失效"是同一类事故。失败数返回给调用方（cmd/pay 的对账循环）打日志告警。
func (s *Service) ReconcileStale(ctx context.Context, before time.Time, limit int) (advanced int, failed int, err error) {
	orders, err := s.store.ListStaleCreated(before, limit)
	if err != nil {
		return 0, 0, err
	}
	var samples []string
	for i := range orders {
		moved, e := s.ReconcileOne(ctx, &orders[i])
		if e != nil {
			failed++
			if len(samples) < reconcileFailureSamples {
				samples = append(samples, fmt.Sprintf("pay_order_id=%d: %v", orders[i].ID, e))
			}
			continue
		}
		if moved {
			advanced++ // 只统计真的推进了状态的；no-op（还没付 / 资金在路上）不算
		}
	}
	if failed > 0 {
		// 日志先行：它落本地文件，是 NATS 挂掉时唯一还能出声的通道。
		allFailed := failed == len(orders)
		logErrorf("pay reconcile: 本轮扫描 %d 单、失败 %d 单（全部失败=%v，多半是凭证配错或通道整体不可用）样本: %v",
			len(orders), failed, allFailed, samples)
		s.publish(EventReconcileFailed, 0, ReconcileSummaryEvent{
			Scanned: len(orders), Failed: failed, AllFailed: allFailed, Samples: samples,
		})
	}
	return advanced, failed, nil
}

// QueryOrder 查支付单。
func (s *Service) QueryOrder(id int64) (*model.PayOrder, error) { return s.store.GetOrder(id) }

// QueryRefund 查退款单。
func (s *Service) QueryRefund(id int64) (*model.PayRefund, error) { return s.store.GetRefund(id) }

func (s *Service) notifyURL(channelCode string) string {
	return s.notifyURLBase + "/v1/pay/notify/" + channelCode
}

// returnBridgeURL 拼装 /v1/pay/return/:channel 的完整地址（含 query），供需要
// 二次确认扣款的通道（RedirectCapturer）引导用户跳转回来时使用。
func (s *Service) returnBridgeURL(channelCode string, payOrderID int64, returnURL string) string {
	u := fmt.Sprintf("%s/v1/pay/return/%s?pay_order_id=%d", s.notifyURLBase, channelCode, payOrderID)
	if returnURL != "" {
		u += "&return_url=" + url.QueryEscape(returnURL)
	}
	return u
}

// publish 发布事件，并且绝不静默吞掉发布失败。
//
// 事件是支付结果通往业务侧的唯一通路，丢一条就是一次事故——最严重的是 EventOrderPaid：
// 订单已经 MarkPaid（钱确实收了），事件却没送出去，网关的充值单永远收不到结果、用户余额
// 永不增加；而对账只扫 CREATED 状态的单，这张已 PAID 的单再也不会被扫到，**永久丢账，
// 零告警**。触发它只需要 NATS 抖一下（重启 / stream 未就绪 / ack 超时）。
//
// 这里只能记日志：日志落本地文件，恰恰是 NATS 挂掉时唯一还能出声的通道。把 error 往上抛
// 是行不通的——webhook 里上抛会让第三方重推，但重推时 MarkPaid 的 CAS 返回 advanced=false，
// 就不会再走到 publish，事件照样丢，只是白白多几轮 400。幂等设计和事件补发在这里是冲突的。
//
// 根治要给业务侧加一条不依赖消息中间件的对账腿（网关的 PENDING 充值单超时后主动反查支付单，
// 是 PAID 就结算——SettleTopup 本身就是幂等 CAS）。
func (s *Service) publish(subject string, payOrderID int64, event any) {
	if err := s.notifier.Publish(subject, event); err != nil {
		logErrorf("pay: 事件发布失败，业务侧将收不到此结果 subject=%s pay_order_id=%d: %v", subject, payOrderID, err)
	}
}

// logErrorf 记录错误日志。logger.L 在单测里未初始化（nil），此时退化为无操作；
// 生产路径由各 cmd 的 logger.Init() 保证可用。
func logErrorf(format string, args ...any) {
	if logger.L == nil {
		return
	}
	logger.L.Errorf(format, args...)
}

// inconsistentEvent 构造"订单状态与实际不一致"告警：库写失败导致的脏状态，需要人来看。
func (s *Service) inconsistentEvent(o *model.PayOrder, reason string) AlertEvent {
	return AlertEvent{
		PayOrderID: o.ID,
		BizType:    o.BizType,
		BizOrderID: o.BizOrderID,
		Channel:    o.Channel,
		Status:     o.Status,
		Reason:     reason,
	}
}

func (s *Service) orderEvent(o *model.PayOrder, status, channelTradeNo string) OrderEvent {
	return OrderEvent{
		PayOrderID:     o.ID,
		BizType:        o.BizType,
		BizOrderID:     o.BizOrderID,
		Channel:        o.Channel,
		Amount:         o.Amount,
		Currency:       o.Currency,
		Status:         status,
		ChannelTradeNo: channelTradeNo,
	}
}

func (s *Service) refundEvent(o *model.PayOrder, r *model.PayRefund, status string) RefundEvent {
	return RefundEvent{
		PayOrderID:  o.ID,
		RefundID:    r.ID,
		BizType:     o.BizType,
		BizOrderID:  o.BizOrderID,
		BizRefundID: r.BizRefundID,
		Amount:      r.Amount,
		Currency:    r.Currency,
		Status:      status,
	}
}
