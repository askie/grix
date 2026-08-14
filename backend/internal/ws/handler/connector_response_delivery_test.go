package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

// 解析 extra 的 connector.response_delivery，便于断言。
func connectorResponseDeliveryOf(t *testing.T, extra json.RawMessage) (string, bool) {
	t.Helper()
	if len(extra) == 0 {
		return "", false
	}
	var envelope struct {
		Connector struct {
			ResponseDelivery string `json:"response_delivery"`
		} `json:"connector"`
	}
	if err := json.Unmarshal(extra, &envelope); err != nil {
		t.Fatalf("unmarshal extra failed: %v", err)
	}
	if envelope.Connector.ResponseDelivery == "" {
		return "", false
	}
	return envelope.Connector.ResponseDelivery, true
}

func TestApplyConnectorResponseDelivery_EmptyExtra(t *testing.T) {
	out := applyConnectorResponseDelivery(nil, connectorResponseDeliverySingle)
	mode, ok := connectorResponseDeliveryOf(t, out)
	if !ok || mode != connectorResponseDeliverySingle {
		t.Fatalf("expected response_delivery=%q, got %q (ok=%v)", connectorResponseDeliverySingle, mode, ok)
	}
}

func TestApplyConnectorResponseDelivery_PreservesTopLevelFields(t *testing.T) {
	// 顶层含大整数（> 2^53），验证不经 float64 round-trip 而精度无损。
	const bigID = "9007199254740993"
	in := json.RawMessage(`{"foo":"bar","big":` + bigID + `}`)

	out := applyConnectorResponseDelivery(in, connectorResponseDeliverySingle)

	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal out failed: %v", err)
	}
	if string(got["foo"]) != `"bar"` {
		t.Fatalf("foo not preserved: %s", got["foo"])
	}
	if strings.TrimSpace(string(got["big"])) != bigID {
		t.Fatalf("big int precision lost: got %s want %s", got["big"], bigID)
	}
	mode, ok := connectorResponseDeliveryOf(t, out)
	if !ok || mode != connectorResponseDeliverySingle {
		t.Fatalf("expected response_delivery=%q, got %q (ok=%v)", connectorResponseDeliverySingle, mode, ok)
	}
}

func TestApplyConnectorResponseDelivery_PreservesExistingConnectorKeys(t *testing.T) {
	in := json.RawMessage(`{"connector":{"thinking_events":"drop","tool_events":"drop"}}`)

	out := applyConnectorResponseDelivery(in, connectorResponseDeliverySingle)

	var envelope struct {
		Connector map[string]string `json:"connector"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		t.Fatalf("unmarshal out failed: %v", err)
	}
	if envelope.Connector["thinking_events"] != "drop" || envelope.Connector["tool_events"] != "drop" {
		t.Fatalf("existing connector keys not preserved: %+v", envelope.Connector)
	}
	if envelope.Connector["response_delivery"] != connectorResponseDeliverySingle {
		t.Fatalf("response_delivery not set: %+v", envelope.Connector)
	}
}
