package network

import (
	"errors"
	"io"
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
	if h.ID > 10000000 || h.Length < 0 || h.Length > 1024*1024 {
		base.Zap().Sugar().Warnf("message error: id(%d),len(%d)", h.ID, h.Length)
		return nil, nil, errors.New("message not in range")
	}
	//base.Zap().Sugar().Debugf("recv message : id(%d),len(%d)", h.ID, h.Length)
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
			err := s.BlockAcceptOne(proc)
			if err != nil {
				base.Zap().Sugar().Errorf("accept error :%s", err.Error())
				break
			}
		}

	} else {
		base.Zap().Sugar().Errorf("create listener first")
	}
}

// BlockAcceptOne 接受一个连接
func (s *Server) BlockAcceptOne(proc *Processor) error {
	if s.Listener != nil {
		conn, err := s.Listener.Accept()
		if err == nil {
			base.Zap().Sugar().Debugf("incomming connection :%s", conn.RemoteAddr().String())
			peer := &ClientPeer{
				Connection:   conn,
				redirectProc: make(chan *Processor, 1),
				Proc:         proc,
			}
			event := &Event{
				ID:   AddEvent,
				Peer: peer,
			}
			proc.EventChan <- event
			go peer.ConnectionHandler()
		}
		return err
	}
	return errors.New("tcpserver accept error :should listen first")
}
