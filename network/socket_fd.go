package network

import (
	"errors"
	"net"
	"syscall"
)

// socketFD returns the descriptor owned by conn without duplicating it.
// The connection must remain open while the descriptor is registered.
func socketFD(conn net.Conn) (int, error) {
	syscallConn, ok := conn.(syscall.Conn)
	if !ok {
		return -1, errors.New("connection does not expose syscall.RawConn")
	}

	raw, err := syscallConn.SyscallConn()
	if err != nil {
		return -1, err
	}

	fd := -1
	if err := raw.Control(func(handle uintptr) {
		fd = int(handle)
	}); err != nil {
		return -1, err
	}
	if fd < 0 {
		return -1, errors.New("invalid socket descriptor")
	}
	return fd, nil
}
