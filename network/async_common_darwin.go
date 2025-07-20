//go:build darwin
// +build darwin

package network

// macOS平台的事件常量（兼容性定义）
const (
	EpollIn  = uint32(1) // 兼容EPOLLIN
	EpollOut = uint32(4) // 兼容EPOLLOUT
	EpollET  = uint32(0) // macOS kqueue本身就是边缘触发
)
