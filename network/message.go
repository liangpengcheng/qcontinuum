package network

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"runtime/debug"
)

var maxMessageLength = 4096

//message head
type MessageHead struct {
	Length int32
	Id     int32
}

//读取消息头
func ReadHead(src []byte) MessageHead {
	buf := bytes.NewBuffer(src)

	buf.Write(src)
	var head MessageHead
	binary.Read(buf, binary.LittleEndian, &head.Length)
	binary.Read(buf, binary.LittleEndian, &head.Id)
	return head
}

//读取消息
func ReadFromConnect(conn net.Conn, length int) ([]byte, error) {
	defer func() {
		if err := recover(); err != nil {
			log.Print(err)
			debug.PrintStack()
		}
	}()

	data := make([]byte, length)
	len, err := io.ReadFull(conn, data)
	if err == nil && len == length {
		return data, nil
	}
	return nil, err
	/*
		bufsize := maxMessageLength
		buf := make([]byte, bufsize)
		i := 0
		for {
			if length < bufsize {
				if length == 0 {
					return data, nil
				}
				remain := make([]byte, length)
				r, err := conn.Read(remain)
				if err != nil {
					return nil, err
				}
				copy(data[i:(i+r)], remain[0:r])
				i += r
				length -= r
			} else {
				r, err := conn.Read(buf)
				if err != nil {
					return nil, err
				}
				copy(data[i:(i+r)], buf[0:r])
				i += r
				length -= r
			}

		}
	*/

}
