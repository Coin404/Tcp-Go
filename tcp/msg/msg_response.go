package msg

import (
	"fmt"
	"net"

	"main/tcp/frame"
	manager "main/tcp/manager"
)

// 发送帧消息
func SendResponse(clientId string, frameResponse *frame.Frame) {
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

// 发送帧消息 - addr
func SendResponseByAddr(remoteAddr string, frameResponse *frame.Frame) {
	// 获取客户端连接
	conn := getClientConnByAddr(remoteAddr)
	if conn == nil {
		fmt.Printf("remoteAddr %s not found\n", remoteAddr)
		return
	}
	fmt.Printf("remoteAddr %s \n", remoteAddr)

	// 发送响应帧
	err := frame.WriteFrame(conn, frameResponse)
	if err != nil {
		fmt.Println("Failed to write response frame:", err)
	} else {
		fmt.Printf("Success response frame to remoteAddr %s \n", remoteAddr)
	}
}

// 发送禁止访问消息
func SendAccessForbidden(remoteAddr string, reason string) {
	response, _ := GenerateConnAuth(false, reason)
	SendResponseByAddr(remoteAddr, response)
	closeClientConnection(remoteAddr, reason)
}

// 逻辑id获取conn连接
func getClientConn(clientId string) net.Conn {
	remoteAddr := manager.GetClientAddr(clientId)
	client, ok := manager.GetClient(remoteAddr)
	if ok {
		return *client.Conn
	}
	return nil
}

// id地址获取conn连接
func getClientConnByAddr(remoteAddr string) net.Conn {
	client, ok := manager.GetClient(remoteAddr)
	if ok {
		return *client.Conn
	}
	return nil
}

// 清除客户端信息
func closeClientConnection(remoteAddr string, reason string) {
	manager.RemoveClient(remoteAddr)
	manager.RemoveClientIdBy(remoteAddr)
	fmt.Printf("Client %s disconnected: %s\n", remoteAddr, reason)
}
