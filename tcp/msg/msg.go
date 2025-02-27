package msg

import (
	"fmt"
	"time"

	"main/tcp/frame"
	message "main/tcp/proto"

	"google.golang.org/protobuf/proto"
)

// 生成心跳消息
func GenerateHeartbeatMessage(clientID string) (*frame.Frame, error) {
	heartbeat := &message.Heartbeat{
		ClientId:  clientID,
		Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
	}

	msg := &message.Message{
		Type: message.MessageType_HEARTBEAT,
		Payload: &message.Message_Heartbeat{
			Heartbeat: heartbeat,
		},
	}

	body, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal heartbeat message: %w", err)
	}

	frameRequest := &frame.Frame{
		Header: frame.FrameHeader{
			Marker:        0xEF,
			Version:       1,
			MessageFlags:  0x02,
			TransactionID: 00002,
			MessageSize:   uint32(len(body)),
		},
		Body: body,
	}

	return frameRequest, nil
}

// 生成普通消息
func GenerateChatMessage(clientID string, receiverID string, content string) (*frame.Frame, error) {
	chatMsg := &message.ChatMessage{
		ClientId:   clientID,
		ReceiverId: receiverID,
		Content:    content,
	}
	msg := &message.Message{
		Type: message.MessageType_CHAT_MESSAGE,
		Payload: &message.Message_ChatMessage{
			ChatMessage: chatMsg,
		},
	}

	body, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	frameRequest := &frame.Frame{
		Header: frame.FrameHeader{
			Marker:        0xEF,
			Version:       1,
			MessageFlags:  0x01,
			TransactionID: 00001,
			MessageSize:   uint32(len(body)),
		},
		Body: body,
	}

	return frameRequest, nil
}

// 生成权限验证消息
func GenerateConnAuth(success bool, content string) (*frame.Frame, error) {
	connAuthMsg := &message.ConnAuth{
		Success: success,
		Message: content,
	}
	msg := &message.Message{
		Type: message.MessageType_CONN_AUTH,
		Payload: &message.Message_ConnAuth{
			ConnAuth: connAuthMsg,
		},
	}

	body, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal connauth message: %w", err)
	}

	frameRequest := &frame.Frame{
		Header: frame.FrameHeader{
			Marker:        0xEF,
			Version:       1,
			MessageFlags:  0x03,
			TransactionID: 00003,
			MessageSize:   uint32(len(body)),
		},
		Body: body,
	}

	return frameRequest, nil
}
