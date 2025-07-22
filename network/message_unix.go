//go:build !windows
// +build !windows

package network

import (
	"syscall"
	"unsafe"

	"github.com/liangpengcheng/qcontinuum/base"
)

// doWrite Unix版本的写入实现
func (w *ZeroCopyMessageWriter) doWrite(req *writeRequest) {
	defer req.buffer.Release()

	for req.offset < req.buffer.Len() {
		n, err := syscall.Write(req.fd, req.buffer.Data()[req.offset:req.buffer.Len()])
		if err != nil {
			if err == syscall.EAGAIN {
				// 暂时无法写入，重新加入队列
				if !w.writeQueue.Push(unsafe.Pointer(req)) {
					base.Zap().Sugar().Warnf("failed to requeue write request")
				}
				return
			}
			base.Zap().Sugar().Warnf("write error: %v", err)
			return
		}
		req.offset += n
	}
}
