//go:build windows
// +build windows

package network

// Windows平台的事件常量（兼容性定义）
const (
	EpollIn  = uint32(1) // 兼容EPOLLIN
	EpollOut = uint32(4) // 兼容EPOLLOUT
	EpollET  = uint32(0) // Windows select不支持边缘触发
)
