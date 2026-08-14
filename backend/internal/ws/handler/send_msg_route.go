package handler

import (
	"context"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func dispatchCrossNode(ctx context.Context, userID int64, cmd string, payload interface{}) {
	remoteNodes, remoteLiveDevices, routeLookupOK := collectLiveRouteNodes(ctx, userID, "", "")
	if routeLookupOK && remoteLiveDevices == 0 && cmd == protocol.CmdPushMsg {
		if err := enqueueOfflinePushTask(userID, cmd, payload); err != nil {
			logger.L.Warnf("enqueue offline push failed user=%d cmd=%s err=%v", userID, cmd, err)
		}
		return
	}

	publishToRouteNodes(ctx, userID, cmd, payload, "", remoteNodes)
}
