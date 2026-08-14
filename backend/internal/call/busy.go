package call

import "fmt"

// UserBusyKey is the cross-node Redis key marking that a user is currently in
// a voice call (value: callID, reserved on invite, released on hangup/leave/
// disconnect). Single source of truth for the key format — used by the WS call
// guard and by agentapi to tag call-turn runs.
func UserBusyKey(userID int64) string {
	return fmt.Sprintf("im:voice:busy:%d", userID)
}
