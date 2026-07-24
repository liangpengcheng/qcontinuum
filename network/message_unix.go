//go:build !windows

package network

import (
	"sync/atomic"
	"syscall"
)

func (w *ZeroCopyMessageWriter) doWrite(req *writeRequest) (bool, error) {
	for req.offset < req.buffer.Len() {
		if atomic.LoadInt32(&w.closed) != 0 {
			return false, syscall.EBADF
		}

		n, err := syscall.Write(req.fd, req.buffer.Data()[req.offset:req.buffer.Len()])
		if n > 0 {
			req.offset += n
			if w.onBytes != nil {
				w.onBytes(n)
			}
		}
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				return true, nil
			}
			return false, err
		}
		if n == 0 {
			return true, nil
		}
	}
	return false, nil
}
