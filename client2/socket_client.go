package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"main/tcp/frame"
	message "main/tcp/proto"

	"google.golang.org/protobuf/proto"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:8888")
	if err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	defer conn.Close()

	fmt.Println("Connected to server. Start sending messages!")

	// 发送初始消息
	if err := sendMessage(conn, "Client2", "Server", "Hello, Server! I am Client2!!!"); err != nil {
		log.Fatalf("Failed to send initial message: %v", err)
	}

	// 启动心跳消息发送协程
	go sendHeartbeat(conn, "Client2")

	// 持续接收服务器响应
	receiveResponses(conn)
}

// sendMessage 构造并发送一个 ChatMessage 消息
func sendMessage(conn net.Conn, clientID, receiverID, content string) error {
	chatMsg := &message.ChatMessage{
		ClientId:   clientID,
		ReceiverId: receiverID,
		Content:    content,
	}
	msg := &message.Message{
		Type: message.MessageType_CHAT_MESSAGE, // 显式设置消息类型
		Payload: &message.Message_ChatMessage{
			ChatMessage: chatMsg,
		},
	}

	body, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
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

	return frame.WriteFrame(conn, frameRequest)
}

// sendHeartbeat 定时发送心跳消息
func sendHeartbeat(conn net.Conn, clientID string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := sendHeartbeatMessage(conn, clientID); err != nil {
				log.Printf("Failed to send heartbeat message: %v", err)
				continue
			}
			fmt.Println("Heartbeat message sent!")
		default:
			time.Sleep(time.Second) // 避免空转
		}
	}
}

// sendHeartbeatMessage 构造并发送一个心跳消息
func sendHeartbeatMessage(conn net.Conn, clientID string) error {
	heartbeat := &message.Heartbeat{
		ClientId:  clientID,
		Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
	}
	msg := &message.Message{
		Type: message.MessageType_HEARTBEAT, // 显式设置消息类型
		Payload: &message.Message_Heartbeat{
			Heartbeat: heartbeat,
		},
	}

	body, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat message: %w", err)
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

	return frame.WriteFrame(conn, frameRequest)
}

// receiveResponses 持续接收服务器响应
func receiveResponses(conn net.Conn) {
	for {
		responseFrame, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Server disconnected.")
				return
			}
			log.Printf("Failed to read response frame: %v", err)
			continue
		}

		// 解析外层的 Message
		var msg message.Message
		if err := proto.Unmarshal(responseFrame.Body, &msg); err != nil {
			log.Printf("Failed to unmarshal outer Message: %v", err)
			continue
		}

		// 根据消息类型解析具体的 payload
		switch msg.Type {
		case message.MessageType_CHAT_MESSAGE:
			// 提取 ChatMessage
			if msg.Payload == nil {
				log.Printf("ChatMessage payload is nil")
				return
			}
			chatMsg, ok := msg.Payload.(*message.Message_ChatMessage)
			if !ok {
				log.Printf("Failed to cast payload to ChatMessage")
				return
			}
			fmt.Printf("Received ChatMessage: %+v\n", chatMsg.ChatMessage)

		case message.MessageType_HEARTBEAT:
			// 提取 Heartbeat
			if msg.Payload == nil {
				log.Printf("Heartbeat payload is nil")
				return
			}
			heartbeat, ok := msg.Payload.(*message.Message_Heartbeat)
			if !ok {
				log.Printf("Failed to cast payload to Heartbeat")
				return
			}
			fmt.Printf("Received Heartbeat: %+v\n", heartbeat.Heartbeat)

		default:
			log.Printf("Unknown message type: %v", msg.Type)
		}
	}
}
