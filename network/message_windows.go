//go:build windows
// +build windows

package network

import (
	"github.com/liangpengcheng/qcontinuum/base"
)

// doWrite Windows版本的写入实现
func (w *ZeroCopyMessageWriter) doWrite(req *writeRequest) {
	defer req.buffer.Release()

	// Windows上使用连接映射来写入
	conn := getConnFromFd(req.fd)
	if conn == nil {
		base.Zap().Sugar().Warnf("connection not found for fd %d", req.fd)
		return
	}

	for req.offset < req.buffer.Len() {
		n, err := conn.Write(req.buffer.Data()[req.offset:req.buffer.Len()])
		if err != nil {
			base.Zap().Sugar().Warnf("write error: %v", err)
			return
		}
		req.offset += n
	}
}
