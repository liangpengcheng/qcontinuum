// +build linux

package network

import "syscall"

// Linux平台的事件常量
const (
	EpollIn  = uint32(syscall.EPOLLIN)
	EpollOut = uint32(syscall.EPOLLOUT)
	EpollET  = uint32(syscall.EPOLLET)
)