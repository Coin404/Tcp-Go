package manager

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// 定义客户端结构体
type Client struct {
	Conn       *net.Conn
	LastActive time.Time
}

// 定义全局的客户端映射表和互斥锁
var (
	clients  = make(map[string]*Client) // 使用IP+端口作为键，客户端结构体作为值
	muClient sync.Mutex                 // 互斥锁，用于线程安全
)

// 添加客户端
func AddClient(remoteAddr string, conn *net.Conn) {
	muClient.Lock()
	defer muClient.Unlock()
	clients[remoteAddr] = &Client{
		Conn:       conn,
		LastActive: time.Now(),
	}
}

// 获取客户端
func GetClient(remoteAddr string) (*Client, bool) {
	muClient.Lock()
	defer muClient.Unlock()
	client, ok := clients[remoteAddr]
	return client, ok
}

// 更新客户端最后活跃时间
func UpdateClientLastActive(remoteAddr string) {
	muClient.Lock()
	defer muClient.Unlock()
	if client, ok := clients[remoteAddr]; ok {
		client.LastActive = time.Now()
	}
}

// 删除客户端
func RemoveClient(remoteAddr string) {
	muClient.Lock()
	defer muClient.Unlock()
	if client, ok := clients[remoteAddr]; ok {
		(*client.Conn).Close()
		delete(clients, remoteAddr)
	}
}

// 清理无效连接
func CleanupInvalidConnections(timeout time.Duration) {
	muClient.Lock()
	defer muClient.Unlock()
	for remoteAddr, client := range clients {
		if time.Since(client.LastActive) > timeout {
			(*client.Conn).Close()
			delete(clients, remoteAddr)
			// 同时删除对应的clientId映射
			RemoveClientIdBy(remoteAddr)
			fmt.Printf("Cleaned up inactive connection: %s\n", remoteAddr)
		}
	}
}
