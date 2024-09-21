package kcp

import (
	"github.com/liangpengcheng/qcontinuum/base"
	"github.com/liangpengcheng/qcontinuum/network"
	_kcp "github.com/xtaci/kcp-go"
)

// Server kcp server
type Server struct {
	Listener *_kcp.Listener
}

// NewKCPServer 创建一个kcp server
func NewKCPServer(host string) (*Server, error) {
	lis, err := _kcp.ListenWithOptions(host, nil, 0, 0)
	if err != nil {
		return nil, err
	}
	// 默认decp
	if err := lis.SetDSCP(0); err != nil {
		lis.Close()
		return nil, err
	}
	if err := lis.SetReadBuffer(4194304); err != nil {
		lis.Close()
		return nil, err
	}
	if err := lis.SetWriteBuffer(4194304); err != nil {
		lis.Close()
		return nil, err
	}
	return &Server{
		Listener: lis,
	}, nil

}

// BlockAccept 阻塞收消息
func (s *Server) BlockAccept(proc *network.Processor) {
	for {
		if err := s.BlockAcceptOne(proc); err != nil {
			base.Zap().Sugar().Errorf("accept error :%s", err.Error())
			break
		}
	}
	base.Zap().Sugar().Infof("exit accept")
}

// BlockAcceptOne 接受一个连接
func (s *Server) BlockAcceptOne(proc *network.Processor) error {
	if conn, err := s.Listener.AcceptKCP(); err == nil {
		base.Zap().Sugar().Infof("remote address:%s", conn.RemoteAddr().String())
		setupKcp(conn)
		peer := &network.ClientPeer{
			Connection:   conn,
			redirectProc: make(chan *network.Processor, 1),
			Proc:         proc,
		}

		go peer.ConnectionHandler()
	} else {
		return err
	}
	return nil
}

func setupKcp(conn *_kcp.UDPSession) {
	conn.SetStreamMode(false)
	conn.SetWriteDelay(false)
	// 这个参数需要好好研究
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetMtu(1400)
	conn.SetWindowSize(4096, 4096)
	conn.SetACKNoDelay(true)
}
