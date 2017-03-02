package network

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
)

var maxMessageLength = 4096

// MessageHead the message head
// length and id
type MessageHead struct {
	Length int32
	ID     int32
}

// ReadHead read the messazge head
// 读取消息头
func ReadHead(src []byte) MessageHead {
	buf := bytes.NewBuffer(src)

	buf.Write(src)
	var head MessageHead
	binary.Read(buf, binary.LittleEndian, &head.Length)
	binary.Read(buf, binary.LittleEndian, &head.ID)
	return head
}

// ReadFromConnect 读取消息
func ReadFromConnect(conn net.Conn, length int) ([]byte, error) {
	data := make([]byte, length)
	len, err := io.ReadFull(conn, data)
	if err == nil && len == length {
		return data, nil
	}
	return nil, err

}
