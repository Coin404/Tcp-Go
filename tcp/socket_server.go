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
	Conn       *net.Conn
	LastActive time.Time
}

var (
	clients          = make(map[string]*Client)      // 使用IP+端口，连接指针作为值
	clientNameToAddr = make(map[string]string)       // 从逻辑ID到IP+端口的映射
	mu               sync.Mutex                      // 互斥锁，用于线程安全
	jobChan          = make(chan *frame.Frame, 1000) // 任务队列，用于处理消息
)

func main() {
	listener, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		fmt.Println("Error starting tcp server :", err)
		return
	}

	defer listener.Close()
	fmt.Println("Server is listening on localhost:8888")

	startWorkerPool()

	for {
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

	// 获取客户端的IP+端口作为唯一凭证
	remoteAddr := conn.RemoteAddr().String()
	mu.Lock()
	clients[remoteAddr] = &Client{
		Conn:       &conn,
		LastActive: time.Now(),
	}
	mu.Unlock()
	fmt.Printf("Client %s connected\n", remoteAddr)

	for {
		frame, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Client disconnected gracefully.")
			} else {
				fmt.Printf("Failed to read frame: %v\n", err)
			}
			logoutClient(remoteAddr)
			return
		}

		// 如果是第一次连接，尝试获取客户端的逻辑名称ID
		clientName := determineClientId(frame.Body)
		if clientName != "" {
			mu.Lock()
			clientNameToAddr[clientName] = remoteAddr
			mu.Unlock()
			fmt.Printf("Client %s registered with name: %s\n", remoteAddr, clientName)
		}

		// frame放入消息队列，防止阻塞
		select {
		case jobChan <- frame:
			fmt.Println("jobChan <- frame success")
		case <-time.After(100 * time.Millisecond):
			fmt.Println("Failed to enqueue frame:timeout")
			// 回复限流消息
			conn.Write([]byte("server busy"))
			continue
		}
	}
}

// 解析首次连接
func determineClientId(data []byte) string {
	// 解析通用 Message 结构
	var msg message.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		fmt.Printf("Failed to unmarshal Message: %v\n", err)
	}
	if message.MessageType_CHAT_MESSAGE == msg.Type {
		// 提取 ChatMessage
		if msg.Payload == nil {
			fmt.Println("ChatMessage payload is nil")
			return ""
		}
		chatMsg, ok := msg.Payload.(*message.Message_ChatMessage)
		if !ok {
			fmt.Println("Failed to cast payload to ChatMessage")
			return ""
		}
		return chatMsg.ChatMessage.ClientId
	} else {
		return ""
	}
}

var singleCoreLimit = 100

// 开启工作池
func startWorkerPool() {
	numCPUs := runtime.NumCPU()
	// 启动一半的CPU核心数作为工作协程
	numWorkers := singleCoreLimit * numCPUs / 2
	fmt.Printf("Starting %d worker goroutines...\n", numWorkers)
	for i := 0; i < numWorkers; i++ {
		go worker()
	}
}

// 消息处理器
func worker() {
	for frame := range jobChan {
		// 解析通用 Message 结构
		var msg message.Message
		if err := proto.Unmarshal(frame.Body, &msg); err != nil {
			fmt.Printf("Failed to unmarshal Message: %v\n", err)
		}

		fmt.Println("Received messageType:", msg.Type)
		switch msg.Type {
		case message.MessageType_CHAT_MESSAGE:
			// 提取 ChatMessage
			if msg.Payload == nil {
				fmt.Println("ChatMessage payload is nil")
				return
			}
			chatMsg, ok := msg.Payload.(*message.Message_ChatMessage)
			if !ok {
				fmt.Println("Failed to cast payload to ChatMessage")
				return
			}
			fmt.Printf("Received ChatMessage: %+v\n", chatMsg.ChatMessage)
			handleChatMessage(chatMsg.ChatMessage)

		case message.MessageType_HEARTBEAT:
			// 提取 Heartbeat
			if msg.Payload == nil {
				fmt.Println("Heartbeat payload is nil")
				return
			}
			heartbeat, ok := msg.Payload.(*message.Message_Heartbeat)
			if !ok {
				fmt.Println("Failed to cast payload to Heartbeat")
				return
			}
			fmt.Printf("Received Heartbeat: %+v\n", heartbeat.Heartbeat)
			handleHeartbeat(heartbeat.Heartbeat)
		default:
			fmt.Println("Unknown message type")
		}
	}
}

func handleChatMessage(msg *message.ChatMessage) {
	response := &message.Message{
		Type: message.MessageType_CHAT_MESSAGE,
		Payload: &message.Message_ChatMessage{
			ChatMessage: &message.ChatMessage{
				ClientId:   "Server",
				ReceiverId: msg.ClientId,
				Content:    "服务器收到消息！！Over",
			},
		},
	}
	sendResponse(msg.ClientId, response)
}

func handleHeartbeat(msg *message.Heartbeat) {
	updateClientLastActive(msg.ClientId)

	response := &message.Message{
		Type: message.MessageType_HEARTBEAT,
		Payload: &message.Message_Heartbeat{
			Heartbeat: &message.Heartbeat{
				ClientId:  msg.ClientId,
				Timestamp: time.Now().Unix(),
			},
		},
	}
	sendResponse(msg.ClientId, response)
}

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
	remoteAddr := clientNameToAddr[clientId]
	clients[remoteAddr] = &Client{
		Conn:       clients[remoteAddr].Conn,
		LastActive: time.Now(),
	}
}

func getClientConn(clientId string) net.Conn {
	mu.Lock()
	defer mu.Unlock()
	remoteAddr := clientNameToAddr[clientId]
	if client, ok := clients[remoteAddr]; ok {
		return *client.Conn
	}
	return nil
}

// 清除客户端信息
func logoutClient(remoteAddr string) {
	mu.Lock()
	delete(clients, remoteAddr)
	for name, addr := range clientNameToAddr {
		if addr == remoteAddr {
			delete(clientNameToAddr, name)
			break
		}
	}
	mu.Unlock()
}
