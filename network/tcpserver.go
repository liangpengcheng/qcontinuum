package network

import (
	"errors"
	"io"
	"log"
	"net"

	"github.com/liangpengcheng/qcontinuum/base"
)

// Server tcp server
type Server struct {
	Listener *net.TCPListener
}

// NewTCP4Server new tcp server
func NewTCP4Server(bindAddress string) (*Server, error) {
	serverAddr, err := net.ResolveTCPAddr("tcp4", bindAddress)
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
func ReadMessage(conn io.Reader) (*MessageHead, []byte, error) {
	buffer, err := ReadFromConnect(conn, 8)
	if err != nil {
		return nil, nil, err
	}
	h := ReadHead(buffer)
	if h.ID > 1024 || h.Length < 0 {
		log.Printf("message error: id(%d),len(%d)", h.ID, h.Length)
		return nil, nil, errors.New("message not in range")
	}
	buffer, err = ReadFromConnect(conn, int(h.Length))
	if err != nil {

		return nil, nil, err
	}
	return &h, buffer, nil

}

// BlockAccept accept
func (s *Server) BlockAccept(proc *Processor) {
	if s.Listener != nil {
		for {
			conn, err := s.Listener.Accept()
			if err == nil {
				base.LogDebug("incomming connection :%s", conn.RemoteAddr().String())
				client := &ClientPeer{
					Connection: conn,
					Serv:       s,
					Flag:       0,
				}
				event := &Event{
					ID:   AddEvent,
					Peer: client,
				}
				proc.EventChan <- event
				go client.ConnectionHandler(proc)
			} else {
				base.LogError("accept error :%s", err.Error())
				break
			}
		}

	} else {
		base.LogError("create listener first")
	}
}
