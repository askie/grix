package agentapi

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/gateway/provisioning"
)

func TestHandleRedisDispatch_RecognizesBroadcastConfigureGatewayProvider(t *testing.T) {
	payload, _ := json.Marshal(provisioning.GatewayProviderConfig{AgentID: 1, APIKey: "k"})
	if !HandleRedisDispatch(provisioning.RedisCmdConfigureGatewayProvider, payload) {
		t.Fatal("expected HandleRedisDispatch to handle broadcast configure_gateway_provider cmd")
	}
}

func TestHandleBroadcastConfigureGatewayProvider_NoopWithoutManagerOrAgentID(t *testing.T) {
	// manager 未初始化(globalManager为nil)或 agentID<=0 时应安全跳过，不panic。
	handleBroadcastConfigureGatewayProvider(provisioning.GatewayProviderConfig{AgentID: 0, APIKey: "k"})
}
