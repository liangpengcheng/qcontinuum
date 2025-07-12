// +build linux

package network

import "syscall"

// Linux平台的事件常量
const (
	EpollIn  = syscall.EPOLLIN
	EpollOut = syscall.EPOLLOUT
	EpollET  = syscall.EPOLLET
)