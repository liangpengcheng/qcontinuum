// +build darwin

package network

// macOS平台的事件常量（兼容性定义）
const (
	EpollIn  = 1  // 兼容EPOLLIN
	EpollOut = 4  // 兼容EPOLLOUT  
	EpollET  = 0  // macOS kqueue本身就是边缘触发
)