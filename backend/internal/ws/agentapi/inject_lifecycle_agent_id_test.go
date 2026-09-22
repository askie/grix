package agentapi

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInjectLifecycleAgentID_PreservesSnowflakeNumber(t *testing.T) {
	const snowflake = "2102381843545849856"
	raw := json.RawMessage(`{"session_id":"sess-1","trigger_msg_id":` + snowflake + `,"running":["evt-1"]}`)

	out := injectLifecycleAgentID(raw, 2057219032343379968)
	require.NotNil(t, out)

	// Original 19-digit number must remain a JSON number (not float-rounded).
	require.Contains(t, string(out), `"trigger_msg_id":`+snowflake)
	require.NotContains(t, string(out), `"trigger_msg_id":2.102`)

	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &obj))
	require.JSONEq(t, `"`+`2057219032343379968`+`"`, string(obj["agent_id"]))
	require.Equal(t, snowflake, string(obj["trigger_msg_id"]))
}

func TestInjectLifecycleAgentID_KeepsExistingAgentID(t *testing.T) {
	raw := json.RawMessage(`{"session_id":"sess-1","agent_id":"111"}`)
	out := injectLifecycleAgentID(raw, 222)
	require.JSONEq(t, `{"session_id":"sess-1","agent_id":"111"}`, string(out))
}
