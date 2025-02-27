package manager

import (
	"net"
	"sync"
	"time"
)

// 定义客户端结构体
type Client struct {
	Conn       *net.Conn
	LastActive time.Time
}

// 定义客户端管理器
type ClientManager struct {
	clients map[string]*Client // 使用IP+端口作为键，客户端结构体作为值
	mu      sync.Mutex         // 互斥锁，用于线程安全
}

// 创建一个新的客户端管理器
func NewClientManager() *ClientManager {
	return &ClientManager{
		clients: make(map[string]*Client),
	}
}

// 添加客户端
func (cm *ClientManager) AddClient(remoteAddr string, conn *net.Conn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.clients[remoteAddr] = &Client{
		Conn:       conn,
		LastActive: time.Now(),
	}
}

// 获取客户端连接
func (cm *ClientManager) GetClient(remoteAddr string) *Client {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.clients[remoteAddr]
}

// 更新客户端最后活跃时间
func (cm *ClientManager) UpdateClientLastActive(remoteAddr string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if client, ok := cm.clients[remoteAddr]; ok {
		client.LastActive = time.Now()
	}
}

// 删除客户端
func (cm *ClientManager) RemoveClient(remoteAddr string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if client, ok := cm.clients[remoteAddr]; ok {
		(*client.Conn).Close()
		delete(cm.clients, remoteAddr)
	}
}

// 清理无效连接
func (cm *ClientManager) CleanupInvalidConnections(timeout time.Duration) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for remoteAddr, client := range cm.clients {
		if time.Since(client.LastActive) > timeout {
			(*client.Conn).Close()
			delete(cm.clients, remoteAddr)
		}
	}
}
