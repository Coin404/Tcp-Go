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

	// 主循环，不断接受客户端连接，处理
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
			mu.Lock()
			// 删除连接队列
			delete(clients, remoteAddr)
			// 同时删除逻辑地址
			for name, addr := range clientNameToAddr {
				if addr == remoteAddr {
					delete(clientNameToAddr, name)
					break
				}
			}
			mu.Unlock()
			return
		}

		// TODO 后续第一次连接的时候需要补充逻辑名称ID，标记客户端A，客户端B，用以AB之间交互
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
	var msg message.Auth
	if err := proto.Unmarshal(data, &msg); err == nil {
		return msg.ClientId
	}
	return ""
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

// 单独的消息处理器
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
	remoteAddr := clientNameToAddr[clientId]
	clients[remoteAddr] = &Client{
		Conn:       clients[remoteAddr].Conn,
		LastActive: time.Now(),
	}
}

// 主动发起连接
func getClientConn(clientId string) net.Conn {
	mu.Lock()
	defer mu.Unlock()
	remoteAddr := clientNameToAddr[clientId]
	return *clients[remoteAddr].Conn
}
