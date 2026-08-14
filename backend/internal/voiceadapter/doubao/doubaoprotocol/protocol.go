// Package doubaoprotocol 实现火山引擎豆包实时语音大模型的二进制协议。
// 移植自官方 Go SDK（去除 portaudio/glog 等外部依赖）。
//
// 协议结构：
//   - Header (4 bytes): version(4b) + header_size(4b) | msg_type(4b) + flags(4b) | serialization(4b) + compression(4b) | reserved(8b)
//   - Payload: [optional event(4B)] [optional session_id_len(4B) + session_id] [payload_len(4B) + payload]
package doubaoprotocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// 协议错误定义
var (
	ErrFrameTooShort    = errors.New("frame too short")
	ErrInvalidMsgType   = errors.New("invalid message type")
	ErrInvalidHeader    = errors.New("invalid header")
	ErrReadPayload      = errors.New("read payload failed")
	ErrRedundantBytes   = errors.New("redundant bytes in data")
)

// MsgType 消息类型
type MsgType int32

const (
	MsgTypeFullClient           MsgType = 1
	MsgTypeAudioOnlyClient      MsgType = 2
	MsgTypeFullServer           MsgType = 3
	MsgTypeAudioOnlyServer      MsgType = 4
	MsgTypeFrontEndResultServer MsgType = 5
	MsgTypeError                MsgType = 6
)

// MsgTypeFlagBits 消息标志位
type MsgTypeFlagBits uint8

const (
	FlagNoSeq       MsgTypeFlagBits = 0
	FlagPositiveSeq MsgTypeFlagBits = 0b1
	FlagLastNoSeq   MsgTypeFlagBits = 0b10
	FlagNegativeSeq MsgTypeFlagBits = 0b11
	FlagWithEvent   MsgTypeFlagBits = 0b100
)

// 版本和 Header 大小
const (
	Version1    uint8 = 0x10 // version=1 << 4
	HeaderSize4 uint8 = 0x01 // header_size=1 (1*4 bytes)
)

// 序列化方式
const (
	SerializationRaw  uint8 = 0x00
	SerializationJSON uint8 = 0x10 // 0b0001 << 4
)

// 压缩方式
const (
	CompressionNone uint8 = 0x00
	CompressionGzip uint8 = 0x01
)

// 事件编号（来自官方SDK）
const (
	EventStartConnection    int32 = 1
	EventFinishConnection   int32 = 2
	EventConnectionStarted  int32 = 50
	EventConnectionFailed   int32 = 51
	EventConnectionFinished int32 = 52
	EventStartSession       int32 = 100
	EventFinishSession      int32 = 102
	EventSessionStarted     int32 = 150
	EventSessionFinished    int32 = 152
	EventSessionFailed      int32 = 153
	EventAudioInput         int32 = 200
	EventSayHello           int32 = 300
	EventTTSSentenceStart   int32 = 350
	EventTTSSentenceDone    int32 = 359
	EventASRSentenceStart   int32 = 450
	EventASRSentenceDone    int32 = 459
	EventChatTTSText        int32 = 500
	EventChatTextQuery      int32 = 501
	EventChatRAGText        int32 = 502
)

// msgType 到 bits 的映射
var msgTypeToBits = map[MsgType]uint8{
	MsgTypeFullClient:           0b0001 << 4,
	MsgTypeAudioOnlyClient:      0b0010 << 4,
	MsgTypeFullServer:           0b1001 << 4,
	MsgTypeAudioOnlyServer:      0b1011 << 4,
	MsgTypeFrontEndResultServer: 0b1100 << 4,
	MsgTypeError:                0b1111 << 4,
}

var bitsToMsgType map[uint8]MsgType

func init() {
	bitsToMsgType = make(map[uint8]MsgType, len(msgTypeToBits))
	for t, b := range msgTypeToBits {
		bitsToMsgType[b] = t
	}
}

// Message 协议消息
type Message struct {
	Type      MsgType
	TypeFlag  MsgTypeFlagBits
	Event     int32
	SessionID string
	ConnectID string
	Sequence  int32
	ErrorCode uint32
	Payload   []byte
}

// Marshal 将消息序列化为二进制帧
func Marshal(msg *Message, serialization uint8) ([]byte, error) {
	typeBits, ok := msgTypeToBits[msg.Type]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrInvalidMsgType, msg.Type)
	}

	buf := new(bytes.Buffer)

	// Header: 4 bytes
	buf.WriteByte(Version1 | HeaderSize4)          // byte 0: version + header_size
	buf.WriteByte(typeBits | uint8(msg.TypeFlag))  // byte 1: msg_type + flags
	buf.WriteByte(serialization | CompressionNone) // byte 2: serialization + compression
	buf.WriteByte(0x00)                            // byte 3: reserved

	// Payload 部分
	if msg.Type == MsgTypeError {
		binary.Write(buf, binary.BigEndian, msg.ErrorCode)
	}

	if containsSequence(msg.TypeFlag) {
		binary.Write(buf, binary.BigEndian, msg.Sequence)
	}

	if containsEvent(msg.TypeFlag) {
		binary.Write(buf, binary.BigEndian, msg.Event)
		// SessionID（某些事件不需要）
		if !isConnectionEvent(msg.Event) {
			size := uint32(len(msg.SessionID))
			binary.Write(buf, binary.BigEndian, size)
			buf.WriteString(msg.SessionID)
		}
	}

	// Payload data with length prefix
	size := uint32(len(msg.Payload))
	binary.Write(buf, binary.BigEndian, size)
	buf.Write(msg.Payload)

	return buf.Bytes(), nil
}

// Unmarshal 从二进制帧反序列化消息
func Unmarshal(data []byte) (*Message, error) {
	if len(data) < 4 {
		return nil, ErrFrameTooShort
	}

	buf := bytes.NewBuffer(data)

	// 读取 Header
	versionSize, _ := buf.ReadByte()
	_ = versionSize // version + header_size

	typeAndFlag, _ := buf.ReadByte()
	typeBits := typeAndFlag &^ 0b00001111
	msgType, ok := bitsToMsgType[typeBits]
	if !ok {
		return nil, fmt.Errorf("%w: bits=%08b", ErrInvalidMsgType, typeBits)
	}

	serComp, _ := buf.ReadByte()
	_ = serComp // serialization + compression

	// 读取 header padding（header_size=1 表示 4 bytes，已读 3 bytes，还需读 1 byte）
	headerSize := int(versionSize&0x0F) * 4
	remaining := headerSize - 3
	if remaining > 0 {
		buf.Next(remaining)
	}

	msg := &Message{
		Type:     msgType,
		TypeFlag: MsgTypeFlagBits(typeAndFlag & 0b00001111),
	}

	// 根据消息类型读取不同字段
	switch msgType {
	case MsgTypeAudioOnlyClient:
		if containsSequence(msg.TypeFlag) {
			if err := binary.Read(buf, binary.BigEndian, &msg.Sequence); err != nil {
				return nil, fmt.Errorf("read sequence: %w", err)
			}
		}
	case MsgTypeAudioOnlyServer:
		if containsSequence(msg.TypeFlag) {
			if err := binary.Read(buf, binary.BigEndian, &msg.Sequence); err != nil {
				return nil, fmt.Errorf("read sequence: %w", err)
			}
		}
	case MsgTypeError:
		if err := binary.Read(buf, binary.BigEndian, &msg.ErrorCode); err != nil {
			return nil, fmt.Errorf("read error code: %w", err)
		}
	}

	// Event + SessionID
	if containsEvent(msg.TypeFlag) {
		if err := binary.Read(buf, binary.BigEndian, &msg.Event); err != nil {
			return nil, fmt.Errorf("read event: %w", err)
		}
		if !isConnectionEvent(msg.Event) {
			var size uint32
			if err := binary.Read(buf, binary.BigEndian, &size); err != nil {
				return nil, fmt.Errorf("read session_id size: %w", err)
			}
			if size > 0 {
				msg.SessionID = string(buf.Next(int(size)))
			}
		}
		// ConnectionStarted/Failed/Finished 有 ConnectID
		if isConnectionResponseEvent(msg.Event) {
			var size uint32
			if err := binary.Read(buf, binary.BigEndian, &size); err != nil {
				return nil, fmt.Errorf("read connect_id size: %w", err)
			}
			if size > 0 {
				msg.ConnectID = string(buf.Next(int(size)))
			}
		}
	}

	// Payload
	var payloadSize uint32
	if err := binary.Read(buf, binary.BigEndian, &payloadSize); err != nil {
		if err == io.EOF {
			return msg, nil // 无 payload
		}
		return nil, fmt.Errorf("read payload size: %w", err)
	}
	if payloadSize > 0 && payloadSize <= math.MaxUint32 {
		msg.Payload = make([]byte, payloadSize)
		copy(msg.Payload, buf.Next(int(payloadSize)))
	}

	return msg, nil
}

func containsSequence(flag MsgTypeFlagBits) bool {
	return flag&FlagPositiveSeq == FlagPositiveSeq || flag&FlagNegativeSeq == FlagNegativeSeq
}

func containsEvent(flag MsgTypeFlagBits) bool {
	return flag&FlagWithEvent == FlagWithEvent
}

func isConnectionEvent(event int32) bool {
	return event == EventStartConnection || event == EventFinishConnection ||
		event == EventConnectionStarted || event == EventConnectionFailed || event == EventConnectionFinished
}

func isConnectionResponseEvent(event int32) bool {
	return event == EventConnectionStarted || event == EventConnectionFailed || event == EventConnectionFinished
}
