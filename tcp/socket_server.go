// Package main implements a TCP server with message queue processing, connection management, and heartbeat support.
package main

import (
	"fmt"
	"io"
	"main/tcp/frame"           // 自定义帧处理包
	manager "main/tcp/manager" // 客户端连接管理包
	msg_manager "main/tcp/msg" // 消息生成和发送包
	message "main/tcp/proto"   // Protocol Buffers 消息定义
	"runtime"
	"time"

	"google.golang.org/protobuf/proto" // Protocol Buffers 序列化库

	"net" // 网络包，用于TCP连接
)

// 全局变量定义
var (
	// listenAddr 服务器监听地址，使用0.0.0.0:8888表示监听所有网络接口的8888端口
	listenAddr = "0.0.0.0:8888"

	// singleCoreLimit 每个CPU核心允许的最大工作协程数
	singleCoreLimit = 100

	// jobChan 消息处理任务队列，用于解耦连接读取和消息处理
	// 队列大小为1000，超过则会触发超时处理
	jobChan = make(chan struct {
		Frame      *frame.Frame // 接收到的帧数据
		RemoteAddr string       // 发送方的远程地址
	}, 1000)
)

// main 函数是服务器的入口点，负责启动监听器、工作协程池和定时清理器
func main() {
	fmt.Println("Starting TCP server...")

	// 使用TCP协议在指定地址创建监听器
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fmt.Printf("Failed to start TCP server: %v\n", err)
		return
	}

	// 确保在函数返回时关闭监听器，释放资源
	defer listener.Close()

	fmt.Printf("✅ Server is successfully listening on %s\n", listenAddr)

	// 启动工作协程池，用于处理接收到的消息
	startWorkerPool()

	// 启动定时清理器，每分钟清理一次无效连接
	// 使用goroutine避免阻塞主循环
	go startCleanupTimer(1 * time.Minute)
	fmt.Println("✅ Connection cleanup timer started (1 minute interval)")

	// 主循环，持续接受新的客户端连接
	fmt.Println("✅ Waiting for client connections...")
	for {
		// Accept() 会阻塞直到有新的连接到来
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("❌ Error accepting connection: %v\n", err)
			continue
		}

		// 为每个新连接创建一个goroutine处理，实现并发处理
		go handleClient(conn)
		fmt.Printf("📞 New connection accepted: %s\n", conn.RemoteAddr().String())
	}
}

// handleClient 处理单个客户端连接的函数
// 负责读取客户端发送的帧数据，并将其放入任务队列中等待处理
func handleClient(conn net.Conn) {
	// 获取客户端的IP+端口作为唯一标识
	remoteAddr := conn.RemoteAddr().String()

	// 将客户端连接添加到连接管理器中
	manager.AddClient(remoteAddr, &conn)
	fmt.Printf("📥 Client %s connected\n", remoteAddr)

	// 延迟执行的清理函数，确保连接关闭时资源被正确释放
	defer func() {
		// 从连接管理器中移除客户端
		manager.RemoveClient(remoteAddr)
		// 移除clientId到地址的映射关系
		manager.RemoveClientIdBy(remoteAddr)
		fmt.Printf("📤 Client %s disconnected: Connection handler exited\n", remoteAddr)
	}()

	// 持续读取客户端发送的数据
	for {
		// 设置10秒的读取超时，防止连接长时间无响应导致资源占用
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		// 读取一个完整的帧数据
		frameMsg, err := frame.ReadFrame(conn)
		if err != nil {
			if err == io.EOF {
				// 客户端优雅关闭连接
				fmt.Printf("👋 Client %s disconnected gracefully\n", remoteAddr)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// 读取超时，检查连接是否仍然有效
				_, ok := manager.GetClient(remoteAddr)
				if !ok {
					fmt.Printf("❌ Connection %s is closed or not found\n", remoteAddr)
					return
				}
				// 连接仍然有效，继续下一次循环
				continue
			} else {
				// 其他读取错误
				fmt.Printf("❌ Failed to read frame from %s: %v\n", remoteAddr, err)
			}
			// 出现错误，退出循环，触发defer清理
			return
		}

		// 将读取到的帧数据放入任务队列，等待工作协程处理
		// 使用select和超时机制防止队列满时阻塞
		select {
		case jobChan <- struct {
			Frame      *frame.Frame
			RemoteAddr string
		}{frameMsg, remoteAddr}:
			// 帧数据成功放入队列
			fmt.Printf("📨 Frame enqueued successfully for %s\n", remoteAddr)
		case <-time.After(100 * time.Millisecond):
			// 队列已满，发送服务器繁忙消息给客户端
			fmt.Printf("⚠️  Failed to enqueue frame for %s: queue full (timeout)\n", remoteAddr)
			msg_manager.SendAccessForbidden(remoteAddr, "Server Busy")
			continue
		}
	}
}

/*
任务处理模块
负责管理工作协程池和处理接收到的消息
*/

// startWorkerPool 启动工作协程池
// 根据CPU核心数动态计算工作协程数量，每个核心分配singleCoreLimit/2个协程
func startWorkerPool() {
	// 获取当前机器的CPU核心数
	numCPUs := runtime.NumCPU()

	// 计算工作协程总数：每个CPU核心分配 singleCoreLimit/2 个协程
	numWorkers := singleCoreLimit * numCPUs / 2

	fmt.Printf("🔧 Starting worker pool with %d goroutines (based on %d CPU cores)\n", numWorkers, numCPUs)

	// 启动指定数量的工作协程
	for i := 0; i < numWorkers; i++ {
		go worker()
	}

	fmt.Printf("✅ Worker pool started successfully with %d workers\n", numWorkers)
}

// worker 是单个工作协程的处理函数
// 从jobChan中读取任务并处理，实现消息的异步处理
func worker() {
	// 持续从任务队列中读取任务
	for job := range jobChan {
		frame := job.Frame
		remoteAddr := job.RemoteAddr

		// 解析帧数据中的Protocol Buffers消息
		var msg message.Message
		if err := proto.Unmarshal(frame.Body, &msg); err != nil {
			fmt.Printf("❌ Failed to unmarshal Message from %s: %v\n", remoteAddr, err)
			continue
		}

		fmt.Printf("📩 Received messageType: %v from %s\n", msg.Type, remoteAddr)

		// 根据消息类型进行不同的处理
		switch payload := msg.Payload.(type) {
		case *message.Message_ConnLogin:
			// 处理连接认证消息
			fmt.Printf("🔐 Received ConnLogin: %+v\n", payload.ConnLogin)
			handleConnLogin(payload.ConnLogin, remoteAddr)

		case *message.Message_ChatMessage:
			// 处理聊天消息
			fmt.Printf("💬 Received ChatMessage: %+v\n", payload.ChatMessage)
			// 检查客户端ID是否已认证
			if manager.CheckClientId(payload.ChatMessage.ClientId) {
				handleChatMessage(payload.ChatMessage)
				// 更新客户端的最后活跃时间
				updateClientLastActive(payload.ChatMessage.ClientId)
			} else {
				// 未认证客户端，发送禁止访问消息
				msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
			}

		case *message.Message_Heartbeat:
			// 处理心跳消息
			fmt.Printf("❤️ Received Heartbeat: %+v\n", payload.Heartbeat)
			// 检查客户端ID是否已认证
			if manager.CheckClientId(payload.Heartbeat.ClientId) {
				handleHeartbeat(payload.Heartbeat)
				// 更新客户端的最后活跃时间
				updateClientLastActive(payload.Heartbeat.ClientId)
			} else {
				// 未认证客户端，发送禁止访问消息
				msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
			}

		default:
			// 未知消息类型
			fmt.Printf("❓ Unknown message type: %v from %s\n", msg.Type, remoteAddr)
		}
	}
}

// handleConnLogin 处理客户端的连接认证请求
// 验证客户端的accessKey和accessSecret，通过后建立clientId到地址的映射
func handleConnLogin(msg *message.ConnLogin, remoteAddr string) {
	// 从消息中提取认证信息
	clientId := msg.ClientId
	accessKey := msg.AccessKey
	accessSecret := msg.AccessSecret

	fmt.Printf("🔐 Processing login request from %s with clientId: %s\n", remoteAddr, clientId)

	// 检查clientId是否为空
	if clientId == "" {
		fmt.Printf("❌ Login failed for %s: Client ID is empty\n", remoteAddr)
		msg_manager.SendAccessForbidden(remoteAddr, "ClientId Empty")
		return
	}

	// TODO: 实际应用中应替换为更安全的认证机制（如数据库查询、加密验证等）
	// 这里使用简单的硬编码验证
	if accessKey != "coin" || accessSecret != "404" {
		fmt.Printf("❌ Login validation failed for client: %s (accessKey: %s, accessSecret: %s)\n", clientId, accessKey, accessSecret)
		msg_manager.SendAccessForbidden(remoteAddr, "Access Forbidden")
		return
	}

	// 认证通过，建立clientId到地址的映射
	manager.AddClientIdToAddr(clientId, remoteAddr)
	fmt.Printf("✅ Login successful for client: %s\n", clientId)

	// 生成并发送认证成功的响应
	response, err := msg_manager.GenerateConnAuth(true, "Access Allow")
	if err != nil {
		fmt.Printf("❌ Failed to generate conn auth response: %v\n", err)
		return
	}
	msg_manager.SendResponseByAddr(remoteAddr, response)

	// 更新客户端的最后活跃时间
	updateClientLastActive(clientId)
}

// handleChatMessage 处理聊天消息
// 目前实现为简单的回显功能，向发送者返回"I Get"消息
func handleChatMessage(msg *message.ChatMessage) {
	fmt.Printf("💬 Processing chat message from %s to %s: %s\n", msg.ClientId, msg.ReceiverId, msg.Content)

	// 生成回显消息
	response, err := msg_manager.GenerateChatMessage(msg.ClientId, "Go!", "I Get")
	if err != nil {
		fmt.Printf("❌ Failed to generate chat response: %v\n", err)
		return
	}

	// 发送回显消息给客户端
	msg_manager.SendResponse(msg.ClientId, response)
	fmt.Printf("📤 Sent chat response to client: %s\n", msg.ClientId)
}

// handleHeartbeat 处理心跳消息
// 更新客户端活跃时间并返回心跳响应
func handleHeartbeat(msg *message.Heartbeat) {
	fmt.Printf("❤️ Processing heartbeat from client: %s\n", msg.ClientId)

	// 生成心跳响应消息
	response, err := msg_manager.GenerateHeartbeatMessage(msg.ClientId)
	if err != nil {
		fmt.Printf("❌ Failed to generate heartbeat response: %v\n", err)
		return
	}

	// 发送心跳响应给客户端
	msg_manager.SendResponse(msg.ClientId, response)
	fmt.Printf("📤 Sent heartbeat response to client: %s\n", msg.ClientId)
}

/*
客户端维护模块
负责管理客户端的活跃状态和清理无效连接
*/

// updateClientLastActive 更新客户端的最后活跃时间
// 通过clientId获取客户端地址，然后调用manager.UpdateClientLastActive更新时间
func updateClientLastActive(clientId string) {
	// 获取客户端对应的地址
	remoteAddr := manager.GetClientAddr(clientId)
	if remoteAddr == "" {
		fmt.Printf("⚠️  Failed to get remote address for clientId: %s\n", clientId)
		return
	}

	// 更新客户端的最后活跃时间
	manager.UpdateClientLastActive(remoteAddr)
	fmt.Printf("⏰ Updated last active time for client: %s\n", clientId)
}

// startCleanupTimer 启动连接清理定时器
// 每隔指定时间间隔清理一次超过超时时间未活跃的连接
func startCleanupTimer(interval time.Duration) {
	// 创建一个定时器，每隔interval时间触发一次
	ticker := time.NewTicker(interval)

	// 确保在函数返回时停止定时器，释放资源
	defer ticker.Stop()

	// 持续监听定时器事件
	for range ticker.C {
		fmt.Println("🧹 Starting connection cleanup...")

		// 清理超过5分钟未活跃的连接
		timeout := 5 * time.Minute
		manager.CleanupInvalidConnections(timeout)

		fmt.Println("✅ Connection cleanup completed.")
	}
}
