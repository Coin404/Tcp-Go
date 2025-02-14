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
	// 连接到服务器
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

	body, err := proto.Marshal(chatMsg)
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

	// if err := validateFrame(frameRequest); err != nil {
	// 	return fmt.Errorf("failed to validate frame: %w", err)
	// }

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

	body, err := proto.Marshal(heartbeat)
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

	// if err := validateFrame(frameRequest); err != nil {
	// 	return fmt.Errorf("failed to validate frame: %w", err)
	// }

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

		var responseMsg message.ChatMessage
		if err := proto.Unmarshal(responseFrame.Body, &responseMsg); err != nil {
			log.Printf("Failed to unmarshal response message: %v", err)
			continue
		}

		fmt.Printf("Received response: %+v\n", &responseMsg)
	}
}

// validateFrame 验证 Frame 的头部和消息体是否符合预期
// func validateFrame(frame *frame.Frame) error {
// 	if frame.Header.Marker != 0xEF {
// 		return fmt.Errorf("invalid marker: expected 0xEF, got %x", frame.Header.Marker)
// 	}
// 	if frame.Header.Version != 1 {
// 		return fmt.Errorf("invalid version: expected 1, got %d", frame.Header.Version)
// 	}
// 	if frame.Header.MessageFlags != 0x01 && frame.Header.MessageFlags != 0x02 {
// 		return fmt.Errorf("invalid message flags: expected 0x01 or 0x02, got %x", frame.Header.MessageFlags)
// 	}
// 	if frame.Header.MessageSize != uint32(len(frame.Body)) {
// 		return fmt.Errorf("invalid message size: expected %d, got %d", len(frame.Body), frame.Header.MessageSize)
// 	}

// 	return nil
// }
