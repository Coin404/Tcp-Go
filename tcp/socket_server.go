package main

import (
	"fmt"
	"io"
	"main/tcp/frame"
	manager "main/tcp/manager"
	msg_manager "main/tcp/msg"
	message "main/tcp/proto"
	"runtime"
	"time"

	"google.golang.org/protobuf/proto"

	"net"
)

var (
	jobChan = make(chan struct {
		Frame      *frame.Frame
		RemoteAddr string
	}, 1000) // 任务队列，用于处理消息
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
	manager.AddClient(remoteAddr, &conn)
	fmt.Printf("Client %s connected\n", remoteAddr)

	for {
		// 检查连接是否已关闭，连接关闭的话结束循环
		_, ok := manager.GetClient(remoteAddr)

		if !ok {
			fmt.Printf("Connection %s is closed or not found.\n", remoteAddr)
			return
		}

		frameMsg, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Client disconnected gracefully.")
			} else {
				fmt.Printf("Failed to read frame: %v\n", err)
			}
			closeClientConnection(remoteAddr, "Client disconnected")
			return
		}

		// frame放入消息队列，防止阻塞
		select {
		case jobChan <- struct {
			Frame      *frame.Frame
			RemoteAddr string
		}{frameMsg, remoteAddr}:
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
	for range numWorkers {
		go worker()
	}
}

// 消息处理器
func worker() {
	for job := range jobChan {
		frame := job.Frame
		remoteAddr := job.RemoteAddr
		// 解析通用 Message 结构
		var msg message.Message
		if err := proto.Unmarshal(frame.Body, &msg); err != nil {
			fmt.Printf("Failed to unmarshal Message: %v\n", err)
		}
		fmt.Println("Received messageType:", msg.Type)

		// 直接进行类型断言
		switch payload := msg.Payload.(type) {
		case *message.Message_ConnLogin:
			fmt.Printf("Received ConnLogin: %+v\n", payload.ConnLogin)
			handleConnLogin(payload.ConnLogin, remoteAddr)
		case *message.Message_ChatMessage:
			fmt.Printf("Received ChatMessage: %+v\n", payload.ChatMessage)
			if manager.CheckClientId(payload.ChatMessage.ClientId) {
				handleChatMessage(payload.ChatMessage)
			} else {
				msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
			}
		case *message.Message_Heartbeat:
			fmt.Printf("Received Heartbeat: %+v\n", payload.Heartbeat)
			if manager.CheckClientId(payload.Heartbeat.ClientId) {
				handleHeartbeat(payload.Heartbeat)
			} else {
				msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
			}
		default:
			fmt.Println("Unknown message type")
		}
	}
}

func handleConnLogin(msg *message.ConnLogin, remoteAddr string) {
	// 从login中解析处 clientNmae，存储到clientIdToAddr ,视为有效连接
	clientId := msg.ClientId
	accessKey := msg.AccessKey
	accessSecret := msg.AccessSecret

	if clientId == "" {
		fmt.Println("Client ID is empty. Login failed.")
		msg_manager.SendAccessForbidden(remoteAddr, "ClinetId Empty")
		return
	}

	// 登录验证逻辑
	if accessKey != "coin" || accessSecret != "404" {
		fmt.Printf("Login validation failed for client: %s\n", clientId)
		msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
		return
	}

	fmt.Printf("Login Success: %s\n", clientId)
	// 验证通过，存储到 clientIdToAddr
	manager.AddClientIdToAddr(clientId, remoteAddr)

	// 发送login回包
	response, _ := msg_manager.GenerateConnAuth(true, "Access Allow")
	msg_manager.SendResponseByAddr(remoteAddr, response)

	// 更新客户端的最后活跃时间
	updateClientLastActive(clientId)

}

func handleChatMessage(msg *message.ChatMessage) {
	response, _ := msg_manager.GenerateChatMessage(msg.ClientId, "Go!", "I Get")
	msg_manager.SendResponse(msg.ClientId, response)
}

func handleHeartbeat(msg *message.Heartbeat) {
	updateClientLastActive(msg.ClientId)
	response, _ := msg_manager.GenerateHeartbeatMessage(msg.ClientId)
	msg_manager.SendResponse(msg.ClientId, response)
}

/*
客户端维护
*/
// 清除客户端信息
func closeClientConnection(remoteAddr string, reason string) {
	manager.RemoveClient(remoteAddr)
	manager.RemoveClientIdBy(remoteAddr)
	fmt.Printf("Client %s disconnected: %s\n", remoteAddr, reason)
}

// 更新client存活时间
func updateClientLastActive(clientId string) {
	remoteAddr := manager.GetClientAddr(clientId)
	manager.UpdateClientLastActive(remoteAddr)
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

// TODO 定时清理
func cleanupInvalidConnections() {
}
