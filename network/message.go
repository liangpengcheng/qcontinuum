package network

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/golang/protobuf/proto"
	"github.com/liangpengcheng/qcontinuum/base"
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
func ReadFromConnect(conn io.Reader, length int) ([]byte, error) {
	data := make([]byte, length)
	len, err := io.ReadFull(conn, data)
	if err == nil && len == length {
		return data, nil
	}
	return nil, err

}

// GetMessageBuffer 把msg转成buffer
func GetMessageBuffer(msg proto.Message, id int32) ([]byte, error) {
	length := int32(proto.Size(msg))
	bufhead := bytes.NewBuffer([]byte{})
	//buf := Base.BufferPoolGet()
	//defer Base.BufferPoolPut(buf)
	binary.Write(bufhead, binary.LittleEndian, length)
	binary.Write(bufhead, binary.LittleEndian, id)
	buf, err := proto.Marshal(msg)
	if err == nil {
		allbuf := base.BytesCombine(bufhead.Bytes(), buf)
		return allbuf, nil
	}
	return nil, err
}
