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
	// 静态监听地址变量
	listenAddr = "0.0.0.0:8888"
	// 单核最大工作协程数
	singleCoreLimit = 100
	// 任务队列，用于处理消息
	jobChan = make(chan struct {
		Frame      *frame.Frame
		RemoteAddr string
	}, 1000)
)

func main() {
	// 使用静态变量指定的地址创建监听器
	listener, err := net.Listen("tcp", listenAddr)
	defer listener.Close()
	if err != nil {
		fmt.Println("Error starting tcp server :", err)
		return
	}
	fmt.Printf("Server is listening on %s\n", listenAddr)

	// 任务处理器，从任务队列中取出消息进行处理
	startWorkerPool()

	// 启动定时清理器，每分钟清理一次无效连接
	go startCleanupTimer(1 * time.Minute)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}

		// 处理客户端连接
		go handleClient(conn)
	}
}

func handleClient(conn net.Conn) {
	// 获取客户端的IP+端口作为唯一凭证
	remoteAddr := conn.RemoteAddr().String()
	manager.AddClient(remoteAddr, &conn)
	fmt.Printf("Client %s connected\n", remoteAddr)

	defer func() {
		// 当连接处理结束时，确保清理客户端信息
		manager.RemoveClient(remoteAddr)
		manager.RemoveClientIdBy(remoteAddr)
		fmt.Printf("Client %s disconnected: Connection handler exited\n", remoteAddr)
	}()

	for {
		// 设置读取超时，防止长时间阻塞
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		frameMsg, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Client disconnected gracefully.")
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// 读取超时，继续下一次循环，通过manager检查连接是否仍然有效
				_, ok := manager.GetClient(remoteAddr)
				if !ok {
					fmt.Printf("Connection %s is closed or not found.\n", remoteAddr)
					return
				}
				continue
			} else {
				fmt.Printf("Failed to read frame: %v\n", err)
			}
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
			msg_manager.SendAccessForbidden(remoteAddr, "Server Busy")
			continue
		}
	}
}

/*
任务处理
*/

// 任务处理器
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
			fmt.Printf("Failed to unmarshal Message from %s: %v\n", remoteAddr, err)
			continue
		}

		fmt.Printf("Received messageType: %v from %s\n", msg.Type, remoteAddr)

		switch payload := msg.Payload.(type) {
		case *message.Message_ConnLogin:
			// 连接认证
			fmt.Printf("Received ConnLogin: %+v\n", payload.ConnLogin)
			handleConnLogin(payload.ConnLogin, remoteAddr)
		case *message.Message_ChatMessage:
			// 消息转发
			fmt.Printf("Received ChatMessage: %+v\n", payload.ChatMessage)
			if manager.CheckClientId(payload.ChatMessage.ClientId) {
				handleChatMessage(payload.ChatMessage)
				updateClientLastActive(payload.ChatMessage.ClientId)
			} else {
				msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
			}
		case *message.Message_Heartbeat:
			// 心跳回包
			fmt.Printf("Received Heartbeat: %+v\n", payload.Heartbeat)
			if manager.CheckClientId(payload.Heartbeat.ClientId) {
				handleHeartbeat(payload.Heartbeat)
				updateClientLastActive(payload.Heartbeat.ClientId)
			} else {
				msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
			}
		default:
			fmt.Printf("Unknown message type: %v from %s\n", msg.Type, remoteAddr)
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

	// TODO 登录验证逻辑
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
	response, _ := msg_manager.GenerateHeartbeatMessage(msg.ClientId)
	msg_manager.SendResponse(msg.ClientId, response)
}

/*
客户端维护
*/
// 更新client存活时间
func updateClientLastActive(clientId string) {
	remoteAddr := manager.GetClientAddr(clientId)
	manager.UpdateClientLastActive(remoteAddr)
}

// 清理连接定时器
func startCleanupTimer(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		// 清理五分钟内没有连接的机器
		timeout := 5 * time.Minute
		manager.CleanupInvalidConnections(timeout)
		fmt.Println("Cleanup completed.")
	}
}
