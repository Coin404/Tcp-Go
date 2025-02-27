package manager

import (
	"sync"
)

// 定义一个全局的互斥锁和 clientIdToAddr 映射
var (
	clientIdToAddr = make(map[string]string) // 从逻辑ID到IP+端口的映射
	muAddr         sync.Mutex                // 互斥锁，用于线程安全
)

// 添加客户端ID到地址映射
func AddClientIdToAddr(clientId string, remoteAddr string) {
	muAddr.Lock()
	defer muAddr.Unlock()
	clientIdToAddr[clientId] = remoteAddr
}

// 获取客户端ID对应的地址
func GetClientAddr(clientId string) string {
	muAddr.Lock()
	defer muAddr.Unlock()
	return clientIdToAddr[clientId]
}

// 删除客户端ID的映射
func RemoveClientId(clientId string) {
	muAddr.Lock()
	defer muAddr.Unlock()
	delete(clientIdToAddr, clientId)
}

// 通过ip地址删除
func RemoveClientIdBy(remoteAddr string) {
	muAddr.Lock()
	defer muAddr.Unlock()
	for clientId, addr := range clientIdToAddr {
		if addr == remoteAddr {
			delete(clientIdToAddr, clientId)
			break
		}
	}
}

// 检查客户端ID是否存在
func CheckClientId(clientId string) bool {
	muAddr.Lock()
	defer muAddr.Unlock()
	_, exists := clientIdToAddr[clientId]
	return exists
}
