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

	// 构造一个 ChatMessage 消息
	chatMsg := &message.ChatMessage{
		ClientId:   "Client2",
		ReceiverId: "Server",
		Content:    "Hello, Server!",
	}

	// 序列化消息
	body, err := proto.Marshal(chatMsg)
	if err != nil {
		log.Fatalf("Failed to marshal message: %v", err)
	}

	// 创建帧
	frameRequest := &frame.Frame{
		Header: frame.FrameHeader{
			Marker:        0xEF,
			Version:       1,
			MessageFlags:  0x01,
			TransactionID: 12345,
			MessageSize:   uint32(len(body)),
		},
		Body: body,
	}

	// 验证帧
	if err := validateFrame(frameRequest); err != nil {
		log.Fatalf("Failed to validate frame: %v", err)
	}

	// 发送帧
	err = frame.WriteFrame(conn, frameRequest)
	if err != nil {
		log.Fatalf("Failed to write frame: %v", err)
	}

	// 启动心跳消息发送协程
	go sendHeartbeat(conn)

	// 持续接收服务器响应
	for {
		// 设置读取超时时间
		// conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		responseFrame, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Server disconnected.")
				return
			}
			log.Printf("Failed to read response frame: %v", err)
			continue
		}

		// 解析响应消息
		var responseMsg message.ChatMessage
		if err := proto.Unmarshal(responseFrame.Body, &responseMsg); err != nil {
			log.Printf("Failed to unmarshal response message: %v", err)
			continue
		}

		// 打印响应消息
		fmt.Printf("Received response: %+v\n", &responseMsg)
	}
}

// validateFrame 验证 Frame 的头部和消息体是否符合预期
func validateFrame(frame *frame.Frame) error {
	// 验证头部
	if frame.Header.Marker != 0xEF {
		return fmt.Errorf("invalid marker: expected 0xEF, got %x", frame.Header.Marker)
	}
	if frame.Header.Version != 1 {
		return fmt.Errorf("invalid version: expected 1, got %d", frame.Header.Version)
	}
	if frame.Header.MessageFlags != 0x01 {
		return fmt.Errorf("invalid message flags: expected 0x01, got %x", frame.Header.MessageFlags)
	}
	if frame.Header.MessageSize != uint32(len(frame.Body)) {
		return fmt.Errorf("invalid message size: expected %d, got %d", len(frame.Body), frame.Header.MessageSize)
	}

	// 验证 Protobuf 消息
	chatMsg := &message.ChatMessage{}
	if err := proto.Unmarshal(frame.Body, chatMsg); err != nil {
		return fmt.Errorf("failed to unmarshal message: %v", err)
	}

	return nil
}

// sendHeartbeat 定时发送心跳消息
func sendHeartbeat(conn net.Conn) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 构造心跳消息
			heartbeat := &message.Heartbeat{
				ClientId:  "Client2",
				Timestamp: time.Now().UnixNano() / int64(time.Millisecond),
			}

			// 序列化心跳消息
			body, err := proto.Marshal(heartbeat)
			if err != nil {
				log.Printf("Failed to marshal heartbeat message: %v", err)
				continue
			}

			// 创建帧
			frameRequest := &frame.Frame{
				Header: frame.FrameHeader{
					Marker:        0xEF,
					Version:       1,
					MessageFlags:  0x02,  // 使用不同的消息标志来区分心跳消息
					TransactionID: 12346, // 使用不同的事务ID
					MessageSize:   uint32(len(body)),
				},
				Body: body,
			}

			// 发送帧
			if err := frame.WriteFrame(conn, frameRequest); err != nil {
				log.Printf("Failed to write heartbeat frame: %v", err)
				continue
			}

			fmt.Println("Heartbeat message sent!")
		}
	}
}
