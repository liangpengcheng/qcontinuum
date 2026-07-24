//go:build windows

package network

import (
	"errors"
	"sync/atomic"
)

func (w *ZeroCopyMessageWriter) doWrite(req *writeRequest) (bool, error) {
	conn := getConnFromFd(req.fd)
	if conn == nil {
		return false, errors.New("connection not found")
	}

	for req.offset < req.buffer.Len() {
		if atomic.LoadInt32(&w.closed) != 0 {
			return false, errors.New("writer is closed")
		}
		n, err := conn.Write(req.buffer.Data()[req.offset:req.buffer.Len()])
		if n > 0 {
			req.offset += n
			if w.onBytes != nil {
				w.onBytes(n)
			}
		}
		if err != nil {
			return false, err
		}
		if n == 0 {
			return false, errors.New("write made no progress")
		}
	}
	return false, nil
}
