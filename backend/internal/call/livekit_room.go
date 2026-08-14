package call

import (
	"context"
	"fmt"
	"time"

	"github.com/livekit/protocol/auth"
	livekit "github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// LiveKitRoomManager 实现 RoomProvider，对接 LiveKit Server。
type LiveKitRoomManager struct {
	roomClient *lksdk.RoomServiceClient
	apiKey     string
	apiSecret  string
	wsURL      string
	publicURL  string
}

// NewLiveKitRoomManager 创建 LiveKit Room Manager。
func NewLiveKitRoomManager(wsURL, publicURL, apiKey, apiSecret string) *LiveKitRoomManager {
	clientURL := publicURL
	if clientURL == "" {
		clientURL = wsURL
	}
	callLogInfof("call trace: livekit room manager init ws_url=%s public_url=%s", wsURL, clientURL)
	return &LiveKitRoomManager{
		roomClient: lksdk.NewRoomServiceClient(wsURL, apiKey, apiSecret),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		wsURL:      wsURL,
		publicURL:  clientURL,
	}
}

// CreateRoom 创建 LiveKit 房间，返回 caller 和 callee 的 JWT token。
func (m *LiveKitRoomManager) CreateRoom(ctx context.Context, callID, callerID, calleeID int64) (tokenCaller, tokenCallee, roomURL string, err error) {
	roomName := fmt.Sprintf("call-%d", callID)
	callLogInfof("call trace: livekit create_room begin call=%d caller=%d callee=%d room=%s", callID, callerID, calleeID, roomName)
	_, err = m.roomClient.CreateRoom(ctx, &livekit.CreateRoomRequest{
		Name:            roomName,
		EmptyTimeout:    60, // 60s 无人自动销毁
		MaxParticipants: 4,
	})
	if err != nil {
		callLogInfof("call trace: livekit create_room error call=%d err=%v", callID, err)
		return "", "", "", fmt.Errorf("livekit create room: %w", err)
	}

	tokenCaller, err = m.issueToken(roomName, fmt.Sprintf("user-%d", callerID), true)
	if err != nil {
		callLogInfof("call trace: livekit issue_token error call=%d identity=user-%d err=%v", callID, callerID, err)
		return "", "", "", err
	}
	tokenCallee, err = m.issueToken(roomName, fmt.Sprintf("user-%d", calleeID), true)
	if err != nil {
		callLogInfof("call trace: livekit issue_token error call=%d identity=user-%d err=%v", callID, calleeID, err)
		return "", "", "", err
	}
	callLogInfof("call trace: livekit create_room done call=%d", callID)
	return tokenCaller, tokenCallee, m.publicURL, nil
}

// CloseRoom 删除 LiveKit 房间。
func (m *LiveKitRoomManager) CloseRoom(ctx context.Context, callID int64) error {
	roomName := fmt.Sprintf("call-%d", callID)
	_, err := m.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: roomName})
	if err != nil {
		callLogInfof("call trace: livekit close_room error call=%d err=%v", callID, err)
	}
	return err
}

// issueToken 签发 LiveKit JWT。
func (m *LiveKitRoomManager) issueToken(room, identity string, canPublish bool) (string, error) {
	at := auth.NewAccessToken(m.apiKey, m.apiSecret)
	grant := &auth.VideoGrant{
		RoomJoin:     true,
		Room:         room,
		CanPublish:   &canPublish,
		CanSubscribe: boolPtr(true),
	}
	at.SetVideoGrant(grant).
		SetIdentity(identity).
		SetValidFor(2 * time.Hour)
	return at.ToJWT()
}

func boolPtr(b bool) *bool { return &b }
