//go:build !darwin && !linux && !windows
// +build !darwin,!linux,!windows

package network

import (
	"net"
	"syscall"
)

// 其他平台的空实现
func setConnForFd(fd int, conn net.Conn) {
	// 空实现
}

func getConnFromFd(fd int) net.Conn {
	return nil
}

func removeConnFromFd(fd int) {
	// 空实现
}

// setNonblock 设置文件描述符为非阻塞
func setNonblock(fd int) error {
	if fd == -1 {
		return nil // 虚拟fd，跳过
	}
	return syscall.SetNonblock(fd, true)
}
