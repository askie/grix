// Package httpapi 暴露支付系统的 HTTP 接口：下单 / 查单 / 退款 / 第三方回调。
package httpapi

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pay"
)

// Handler 持有支付核心 Service。
type Handler struct {
	Svc *pay.Service
}

// Register 挂载支付路由到 /v1/pay。管理面（下单/查单/退款）要求服务间共享密钥，
// 第三方回调与用户跳转（notify/refund-notify/return）保持公网可达、不鉴权。
func Register(r *gin.Engine, h *Handler, internalToken string) {
	g := r.Group("/v1/pay", RequireInternalToken(internalToken))
	g.POST("/orders", h.CreateOrder)
	g.GET("/orders/:id", h.GetOrder)
	g.POST("/refunds", h.CreateRefund)
	g.GET("/refunds/:id", h.GetRefund)
	pub := r.Group("/v1/pay")
	pub.POST("/notify/:channel", h.Notify)
	pub.POST("/refund-notify/:channel", h.RefundNotify)
	pub.GET("/return/:channel", h.Return)
}

type createOrderBody struct {
	BizType    string            `json:"biz_type" binding:"required"`
	BizOrderID string            `json:"biz_order_id" binding:"required"`
	Channel    string            `json:"channel" binding:"required"`
	Amount     decimal.Decimal   `json:"amount"`
	Currency   string            `json:"currency" binding:"required"`
	Subject    string            `json:"subject"`
	ReturnURL  string            `json:"return_url"`
	Extra      map[string]string `json:"extra"`
}

// CreateOrder 创建支付单并返回收银台跳转。
func (h *Handler) CreateOrder(c *gin.Context) {
	var body createOrderBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.Svc.CreateOrder(c.Request.Context(), pay.CreateOrderRequest{
		BizType:    body.BizType,
		BizOrderID: body.BizOrderID,
		Channel:    body.Channel,
		Amount:     body.Amount,
		Currency:   body.Currency,
		Subject:    body.Subject,
		ReturnURL:  body.ReturnURL,
		Extra:      body.Extra,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pay_order_id":     strconv.FormatInt(res.PayOrderID, 10),
		"status":           res.Status,
		"pay_url":          res.PayURL,
		"channel_trade_no": res.ChannelTradeNo,
	})
}

// GetOrder 查支付单。
func (h *Handler) GetOrder(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	o, err := h.Svc.QueryOrder(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "order not found"})
		return
	}
	c.JSON(http.StatusOK, o)
}

type createRefundBody struct {
	PayOrderID  string          `json:"pay_order_id" binding:"required"`
	BizRefundID string          `json:"biz_refund_id" binding:"required"`
	Amount      decimal.Decimal `json:"amount"`
	Reason      string          `json:"reason"`
}

// CreateRefund 发起退款。
func (h *Handler) CreateRefund(c *gin.Context) {
	var body createRefundBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	payOrderID, err := strconv.ParseInt(body.PayOrderID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pay_order_id"})
		return
	}
	r, err := h.Svc.CreateRefund(c.Request.Context(), pay.RefundRequest{
		PayOrderID:  payOrderID,
		BizRefundID: body.BizRefundID,
		Amount:      body.Amount,
		Reason:      body.Reason,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, r)
}

// GetRefund 查退款单。
func (h *Handler) GetRefund(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	r, err := h.Svc.QueryRefund(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "refund not found"})
		return
	}
	c.JSON(http.StatusOK, r)
}

// Notify 接收第三方异步通知。成功回 "success"（支付宝要求纯文本），
// 失败回非 200 让第三方重推，最终由对账兜底。
func (h *Handler) Notify(c *gin.Context) {
	channelCode := c.Param("channel")
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, "read body failed")
		return
	}
	if err := h.Svc.HandleNotify(c.Request.Context(), channelCode, c.Request.Header, raw); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

// RefundNotify 接收第三方异步退款结果通知（如 PayPal PAYMENT.CAPTURE.REFUNDED
// webhook）。支付宝退款为同步返回，一般不会打到这条路由。
func (h *Handler) RefundNotify(c *gin.Context) {
	channelCode := c.Param("channel")
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(http.StatusBadRequest, "read body failed")
		return
	}
	if err := h.Svc.HandleRefundNotify(c.Request.Context(), channelCode, c.Request.Header, raw); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

// Return 承接用户从收银台跳转回来的请求。对需要二次确认扣款的通道（如 PayPal
// Orders v2）主动完成确认；不需要的通道（如支付宝）直接放行。完成后 302 到
// query 里的 return_url（不带则回一段纯文本提示），并附带 status，取值见 returnStatusOf：
// success（已到账）/ pending（已受理但钱还没到，如 PayPal 延审）/ failed / closed。
func (h *Handler) Return(c *gin.Context) {
	payOrderID, err := strconv.ParseInt(c.Query("pay_order_id"), 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid pay_order_id")
		return
	}
	returnURL := c.Query("return_url")
	// status 必须按订单的真实状态派生，不能只看二次确认有没有报错。PayPal 反欺诈延审时
	// capture 会被正常受理（不报错），但钱还没到、订单仍是 CREATED——此时报 success 就是
	// 谎报到账，用户很可能以为没付成功而再付一次（充值单每次都是新的 biz_order_id，幂等
	// 拦不住），或者前端照着这个参数弹出"充值成功"。
	status := "failed"
	if order, err := h.Svc.CompleteRedirect(c.Request.Context(), payOrderID); err == nil {
		status = returnStatusOf(order.Status)
	}
	// 只跳回白名单内的地址（同域前端）。空串或外域一律退化为纯文本完成页，
	// 防止本端点被当作开放重定向拿去钓鱼（return_url 源自业务调用方，不可信）。
	if !h.Svc.ReturnURLAllowed(returnURL) {
		c.String(http.StatusOK, "支付处理完成（%s），请返回原页面。", status)
		return
	}
	sep := "?"
	if strings.Contains(returnURL, "?") {
		sep = "&"
	}
	c.Redirect(http.StatusFound, returnURL+sep+"status="+status)
}

// returnStatusOf 把订单状态映射成跳回前端时带的 status 参数。
// pending 表示"已受理但钱还没到"（PayPal 延审 / e-check）：既不是成功也不是失败，
// 前端应提示"处理中"而不是"充值成功"，否则用户会以为没付上而重复付款。
func returnStatusOf(orderStatus string) string {
	switch orderStatus {
	case model.PayOrderStatusPaid:
		return "success"
	case model.PayOrderStatusFailed:
		return "failed"
	case model.PayOrderStatusClosed:
		return "closed"
	default:
		return "pending"
	}
}
