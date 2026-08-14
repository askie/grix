package service

import (
	"fmt"

	"github.com/google/uuid"
)

func newSessionID() string {
	return uuid.NewString()
}

// buildDirectKey creates a canonical dedupe key for 1:1 sessions.
// Participant A is always the human caller (type=1), participant B is peerType/peerID.
func buildDirectKey(userID, peerID int64, peerType int16) string {
	typeA := int16(1)
	idA := userID
	typeB := peerType
	idB := peerID

	if typeA > typeB || (typeA == typeB && idA > idB) {
		typeA, typeB = typeB, typeA
		idA, idB = idB, idA
	}

	return fmt.Sprintf("d:%d:%d|%d:%d", typeA, idA, typeB, idB)
}
