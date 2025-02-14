package main

import (
	"fmt"
	"io"
	"main/tcp/frame"
	message "main/tcp/proto"
	"runtime"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"net"
)

// 定义一个结构体，用于存储客户端的连接和最后活跃时间
type Client struct {
	Conn       net.Conn
	LastActive time.Time
}

var (
	clients = make(map[string]Client)       // 使用客户端ID作为键，连接作为值
	mu      sync.Mutex                      // 互斥锁，用于线程安全
	jobChan = make(chan *frame.Frame, 1000) // 任务队列，用于处理消息
)

func main() {
	// 指令 tcp 连接 ， 指定监听端口
	listener, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		fmt.Println("Error starting tcp server :", err)
		return
	}

	// 确保在程序退出的时候关闭连接
	defer listener.Close()
	fmt.Println("Server is listening on localhost:8888")

	// 启动工作池
	startWorkerPool()

	// 主循环，不断接受客户端连接
	for {
		// 接受客户端连接
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}

		// 使用goroutine处理并发连接，实现并发处理
		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	defer func() {
		// 在客户端断开连接时关闭连接
		conn.Close()
	}()

	clientId := ""
	for {
		frame, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Client disconnected gracefully.")
				return
			}
			fmt.Printf("Failed to read frame: %v\n", err)
			// 如果已经初始化了clientId，则从clients中移除
			if clientId != "" {
				mu.Lock()
				delete(clients, clientId)
				mu.Unlock()
			}
			return
		}

		// 如果clientId为空，说明是第一次连接，需要初始化clientId
		if clientId == "" {
			clientId = determineClientId(frame.Body)
			if clientId == "" {
				fmt.Println("Failed to determine clientId from the first message")
				return
			}
			mu.Lock()
			clients[clientId] = Client{
				Conn:       conn,
				LastActive: time.Now(),
			}
			mu.Unlock()
			fmt.Printf("Client %s connected\n", clientId)
		}

		// 将消息放入任务队列
		jobChan <- frame
	}
}

// 解析首次连接
func determineClientId(data []byte) string {
	var msg message.Auth
	if err := proto.Unmarshal(data, &msg); err == nil {
		return msg.ClientId
	}
	return ""
}

// 开启工作池
func startWorkerPool() {
	// 获取当前CPU核心数
	numCPUs := runtime.NumCPU()
	// 启动一半的CPU核心数作为工作协程
	numWorkers := numCPUs / 2
	fmt.Printf("Starting %d worker goroutines...\n", numWorkers)
	for i := 0; i < numWorkers; i++ {
		go worker()
	}
}

// 消息处理器
func worker() {
	for frame := range jobChan {
		// 解析 Protobuf 消息
		messageType := determineMessageType(frame.Header)
		switch messageType {
		case "ChatMessage":
			var msg message.ChatMessage
			if err := proto.Unmarshal(frame.Body, &msg); err != nil {
				fmt.Println("Failed to unmarshal ChatMessage:", err)
				continue
			}
			fmt.Printf("Received ChatMessage: %+v\n", &msg)
			handleChatMessage(&msg)

		case "Heartbeat":
			var msg message.Heartbeat
			if err := proto.Unmarshal(frame.Body, &msg); err != nil {
				fmt.Println("Failed to unmarshal Heartbeat:", err)
				continue
			}
			fmt.Printf("Received Heartbeat: %+v\n", &msg)
			handleHeartbeat(&msg)

		case "Auth":
			var msg message.Auth
			if err := proto.Unmarshal(frame.Body, &msg); err != nil {
				fmt.Println("Failed to unmarshal Auth:", err)
				continue
			}
			fmt.Printf("Received Auth: %+v\n", &msg)
			handleAuth(&msg)

		default:
			fmt.Println("Unknown message type")
		}
	}
}

// 判断是哪一种消息格式
func determineMessageType(header frame.FrameHeader) string {
	switch header.MessageFlags {
	case 0x01:
		return "ChatMessage"
	case 0x02:
		return "Heartbeat"
	case 0x03:
		return "Auth"
	default:
		return "Unknown"
	}
}

func handleChatMessage(msg *message.ChatMessage) {
	// 处理 ChatMessage 逻辑
	response := &message.ChatMessage{
		ClientId:   "Server",
		ReceiverId: msg.ClientId,
		Content:    "服务器收到消息！！Over",
	}
	sendResponse(msg.ClientId, response)
}

func handleHeartbeat(msg *message.Heartbeat) {
	updateClientLastActive(msg.ClientId)

	// 发送回复消息
	response := &message.Heartbeat{
		ClientId:  msg.ClientId,
		Timestamp: time.Now().Unix(),
	}
	sendResponse(msg.ClientId, response)
}

func handleAuth(msg *message.Auth) {
	// 处理 Auth 逻辑
	// 示例：发送一个响应消息
	response := &message.Auth{
		ClientId: msg.ClientId,
		Token:    "Authenticated",
	}
	sendResponse(msg.ClientId, response)
}

// 回包
func sendResponse(clientId string, response proto.Message) {
	// 序列化响应消息
	body, err := proto.Marshal(response)
	if err != nil {
		fmt.Println("Failed to marshal response:", err)
		return
	}

	// 创建响应帧
	frameResponse := &frame.Frame{
		Header: frame.FrameHeader{
			Marker:        0xEF,
			Version:       1,
			MessageFlags:  0x01,
			TransactionID: 12345,
			MessageSize:   uint32(len(body)),
		},
		Body: body,
	}

	// 获取客户端连接
	conn := getClientConn(clientId)
	if conn == nil {
		fmt.Printf("Client %s not found\n", clientId)
		return
	}

	// 发送响应帧
	err = frame.WriteFrame(conn, frameResponse)
	if err != nil {
		fmt.Println("Failed to write response frame:", err)
	}
}

// 更新client存活时间
func updateClientLastActive(clientId string) {
	mu.Lock()
	defer mu.Unlock()
	clients[clientId] = Client{
		Conn:       clients[clientId].Conn,
		LastActive: time.Now(),
	}
}

// 主动发起连接
func getClientConn(clientId string) net.Conn {
	mu.Lock()
	defer mu.Unlock()
	return clients[clientId].Conn
}
