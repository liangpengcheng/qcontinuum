package network

import "net"

type Server struct {
	Listener *net.TCPListener
}
