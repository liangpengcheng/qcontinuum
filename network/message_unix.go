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
	// 注意：不使用defer释放buffer，因为EAGAIN时需要重新入队

	for req.offset < req.buffer.Len() {
		n, err := syscall.Write(req.fd, req.buffer.Data()[req.offset:req.buffer.Len()])
		if err != nil {
			if err == syscall.EAGAIN {
				// 暂时无法写入，重新加入队列（不释放buffer）
				if !w.writeQueue.Push(unsafe.Pointer(req)) {
					base.Zap().Sugar().Warnf("failed to requeue write request")
					req.buffer.Release() // 入队失败才释放
				}
				return
			}
			base.Zap().Sugar().Warnf("write error: %v", err)
			req.buffer.Release() // 写入失败时释放
			return
		}
		req.offset += n
	}

	// 写入完成后释放buffer
	req.buffer.Release()
}
