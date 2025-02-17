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

	// 启动定时清理器，每分钟清理一次无效连接
	go startCleanupTimer(1 * time.Minute)

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

		// TODO，需要定义一个登录接口，记录逻辑id，与remoteAddr绑定
		// 没有绑定的视为野连接，直接释放
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
			fmt.Println("jobChan <- frame success", remoteAddr)
		case <-time.After(100 * time.Millisecond):
			fmt.Println("Failed to enqueue frame:timeout")
			// TODO 回复限流消息
			// conn.Write([]byte("server busy"))
			continue
		}
	}
}

/*
任务处理
*/

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

		// 直接进行类型断言
		switch payload := msg.Payload.(type) {
		case *message.Message_ChatMessage:
			fmt.Printf("Received ChatMessage: %+v\n", payload.ChatMessage)
			handleChatMessage(payload.ChatMessage)
		case *message.Message_Heartbeat:
			fmt.Printf("Received Heartbeat: %+v\n", payload.Heartbeat)
			handleHeartbeat(payload.Heartbeat)
		default:
			fmt.Println("Unknown message type")
		}
	}
}

func handleChatMessage(msg *message.ChatMessage) {
	response, _ := generateChatMessage(msg.ClientId, "Go!", "I Get")
	sendResponse(msg.ClientId, response)
}

func handleHeartbeat(msg *message.Heartbeat) {
	updateClientLastActive(msg.ClientId)
	response, _ := generateHeartbeatMessage(msg.ClientId)
	sendResponse(msg.ClientId, response)
}

// 生成心跳消息
func generateHeartbeatMessage(clientID string) (*frame.Frame, error) {
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
func generateChatMessage(clientID string, receiverID string, content string) (*frame.Frame, error) {
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

/*
连接相关
*/
// 发送帧消息
func sendResponse(clientId string, frameResponse *frame.Frame) {
	// 获取客户端连接
	conn := getClientConn(clientId)
	if conn == nil {
		fmt.Printf("Client %s not found\n", clientId)
		return
	}

	// 发送响应帧
	err := frame.WriteFrame(conn, frameResponse)
	if err != nil {
		fmt.Println("Failed to write response frame:", err)
	}
}

// 逻辑id获取conn连接
func getClientConn(clientId string) net.Conn {
	mu.Lock()
	defer mu.Unlock()
	remoteAddr := clientNameToAddr[clientId]
	if client, ok := clients[remoteAddr]; ok {
		return *client.Conn
	}
	return nil
}

/*
客户端维护
*/

// TODO 解析首次连接，后续重新定义首次连接的消息
func determineClientId(data []byte) string {
	var msg message.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		fmt.Printf("Failed to unmarshal Message: %v\n", err)
	}
	if message.MessageType_CHAT_MESSAGE == msg.Type {
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

// 清理连接定时器
func startCleanupTimer(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cleanupInvalidConnections()
		default:
			time.Sleep(time.Second)
		}
	}
}

// 定时清理
func cleanupInvalidConnections() {
	mu.Lock()
	defer mu.Unlock()

	for remoteAddr, client := range clients {
		// 检查 remoteAddr 是否在 clientNameToAddr 中
		isValid := false
		for _, addr := range clientNameToAddr {
			if addr == remoteAddr {
				isValid = true
				break
			}
		}

		// 如果 remoteAddr 不在 clientNameToAddr 中，关闭连接并移除
		if !isValid {
			fmt.Printf("Invalid connection detected: %s. Closing...\n", remoteAddr)
			(*client.Conn).Close()
			delete(clients, remoteAddr)
		}
	}
}
