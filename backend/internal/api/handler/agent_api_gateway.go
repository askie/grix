package handler

import (
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// AgentAPIGatewayWallet 处理 GET /v1/agent-api/gateway/wallet —— connector 以 agent
// api_key 查询本 owner 的中转网关钱包余额，用于工具栏展示中转 Key 余额。
// 记账货币统一为 USD；余额以字符串返回，避免十进制精度在 JSON number 链路中丢失。
// 与 C 端 GET /v1/gateway/wallet 一致：owner 还没有钱包时会自动开一个零余额钱包。
func AgentAPIGatewayWallet(c *gin.Context) {
	ownerID := middleware.GetOwnerID(c)
	w, ec := service.GatewayGetWallet(ownerID)
	if ec != nil {
		response.Fail(c, ec.HTTPStatus, ec.BizCode, ec.Msg)
		return
	}
	response.OK(c, gin.H{
		"balance":  w.Balance.String(),
		"currency": "USD",
	})
}
