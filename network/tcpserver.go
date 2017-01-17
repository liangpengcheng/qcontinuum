package network

import (
	"errors"
	"log"
	"net"
)

// Server tcp server
type Server struct {
	Listener *net.TCPListener
}

// NewTCP4Server new tcp server
func NewTCP4Server(bindAddress string) (*Server, error) {
	serverAddr, err := net.ResolveTCPAddr("tpc4", bindAddress)
	if err != nil {
		return nil, err
	}
	listner, err := net.ListenTCP("tcp", serverAddr)
	if err != nil {
		return nil, err
	}
	server := &Server{
		Listener: listner,
	}
	return server, nil
}

// ReadMessage read a message from connection ,blocked
func ReadMessage(conn net.Conn) (*MessageHead, []byte, error) {
	buffer, err := ReadFromConnect(conn, 8)
	if err != nil {

		return nil, nil, err
	}
	h := ReadHead(buffer)
	if h.ID > 1024 || h.Length < 0 {
		log.Printf("message error: id(%d),len(%d)", h.ID, h.Length)
		conn.Close()
		return nil, nil, errors.New("message not in range")
	}
	buffer, err = ReadFromConnect(conn, int(h.Length))
	if err != nil {

		return nil, nil, err
	}
	return &h, buffer, nil

}
