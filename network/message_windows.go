//go:build windows
// +build windows

package network

import (
	"github.com/liangpengcheng/qcontinuum/base"
)

// doWrite Windows版本的写入实现 - 使用IOCP异步写入
func (w *ZeroCopyMessageWriter) doWrite(req *writeRequest) {
	// 注意：不使用defer释放buffer，保持与Unix版本一致

	// 在Windows IOCP模式下，写入操作应该通过reactor的postWrite方法
	// 这里我们先使用简化的同步写入，后续可以优化为完全异步
	conn := getConnFromFd(req.fd)
	if conn == nil {
		base.Zap().Sugar().Warnf("connection not found for fd %d", req.fd)
		req.buffer.Release()
		return
	}

	for req.offset < req.buffer.Len() {
		n, err := conn.Write(req.buffer.Data()[req.offset:req.buffer.Len()])
		if err != nil {
			base.Zap().Sugar().Warnf("write error: %v", err)
			req.buffer.Release()
			return
		}
		req.offset += n
	}

	// 写入完成后释放buffer
	req.buffer.Release()
}
