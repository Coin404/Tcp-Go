package frame

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
)

// FrameHeader 定义了 TCP 帧的头部结构
type FrameHeader struct {
	Marker        byte   // 帧起始标记，固定为 0xEF
	Version       byte   // 版本号
	MessageFlags  byte   // 消息标志
	TransactionID uint32 // 事务 ID
	MessageSize   uint32 // 消息体大小（单位：字节）
}

// Frame 定义了完整的 TCP 帧结构
type Frame struct {
	Header FrameHeader
	Body   []byte // 消息体（Protobuf 消息序列化后的字节）
}

// Encode 将 Frame 序列化为字节流
func (f *Frame) Encode() ([]byte, error) {
	// 创建一个字节缓冲区
	buffer := make([]byte, 0, 11+len(f.Body)) // 11 字节的头部 + 消息体

	// 写入头部
	buffer = append(buffer, f.Header.Marker)
	buffer = append(buffer, f.Header.Version)
	buffer = append(buffer, f.Header.MessageFlags)
	buffer = append(buffer, byte(f.Header.TransactionID>>24))
	buffer = append(buffer, byte(f.Header.TransactionID>>16))
	buffer = append(buffer, byte(f.Header.TransactionID>>8))
	buffer = append(buffer, byte(f.Header.TransactionID))
	buffer = append(buffer, byte(f.Header.MessageSize>>24))
	buffer = append(buffer, byte(f.Header.MessageSize>>16))
	buffer = append(buffer, byte(f.Header.MessageSize>>8))
	buffer = append(buffer, byte(f.Header.MessageSize))

	// 写入消息体
	buffer = append(buffer, f.Body...)

	return buffer, nil
}

// Decode 从字节流中反序列化出 Frame
func Decode(reader io.Reader) (*Frame, error) {
	// 创建一个 Frame 实例
	frame := &Frame{}

	// 读取头部
	header := make([]byte, 11)
	_, err := io.ReadFull(reader, header)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF // 返回 EOF 错误，表明连接已关闭
		}
		return nil, err
	}

	// 解析头部
	frame.Header.Marker = header[0]
	frame.Header.Version = header[1]
	frame.Header.MessageFlags = header[2]
	frame.Header.TransactionID = binary.BigEndian.Uint32(header[3:7])
	frame.Header.MessageSize = binary.BigEndian.Uint32(header[7:11])

	// 读取消息体
	if frame.Header.MessageSize > 0 {
		frame.Body = make([]byte, frame.Header.MessageSize)
		_, err := io.ReadFull(reader, frame.Body)
		if err != nil {
			if err == io.EOF {
				return nil, io.EOF // 返回 EOF 错误，表明连接已关闭
			}
			return nil, err
		}
	}

	return frame, nil
}

// WriteFrame 将 Frame 写入 TCP 连接
func WriteFrame(conn net.Conn, frame *Frame) error {
	data, err := frame.Encode()
	if err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

// ReadFrame 从 TCP 连接中读取 Frame
func ReadFrame(conn net.Conn) (*Frame, error) {
	bufioreader := bufio.NewReader(conn)
	return Decode(bufioreader)
}
