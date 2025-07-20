//go:build linux
// +build linux

package network

import "syscall"

// Linux平台的事件常量
const (
	EpollIn  = uint32(syscall.EPOLLIN)  // 0x001
	EpollOut = uint32(syscall.EPOLLOUT) // 0x004
	EpollET  = uint32(0x80000000)       // EPOLLET = 0x80000000
)
