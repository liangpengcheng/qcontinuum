package network

import "net"

//ClientPeer client connection peer
type ClientPeer struct {
	Connection net.Conn
	Serv       *Server
	Flag       int32
}
