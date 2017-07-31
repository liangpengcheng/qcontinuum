package network

import (
	"github.com/liangpengcheng/qcontinuum/base"
	kcp "github.com/xtaci/kcp-go"
)

// KcpServer kcp server
type KcpServer struct {
	Listener *kcp.Listener
}

// NewKCPServer 创建一个kcp server
func NewKCPServer(host string) (*KcpServer, error) {
	lis, err := kcp.ListenWithOptions(host, nil, 0, 0)
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
	return &KcpServer{
		Listener: lis,
	}, nil

}

// BlockAccept 阻塞收消息
func (s *KcpServer) BlockAccept(proc *Processor) {
	for {
		if conn, err := s.Listener.AcceptKCP(); err == nil {
			base.LogInfo("remote address:%s", conn.RemoteAddr().String())
			setupKcp(conn)
			peer := &ClientPeer{
				Connection:   conn,
				RedirectProc: make(chan *Processor, 1),
				Proc:         proc,
			}

			go peer.ConnectionHandler()
		} else {
			base.LogError("accept error :%s", err.Error())
			break
		}
	}
	base.LogInfo("exit accept")
}

// BlockAcceptOne 接受一个连接
func (s *KcpServer) BlockAcceptOne(proc *Processor) {
	if conn, err := s.Listener.AcceptKCP(); err == nil {
		base.LogInfo("remote address:%s", conn.RemoteAddr().String())
		setupKcp(conn)
		peer := &ClientPeer{
			Connection: conn,
		}

		go peer.ConnectionHandler()
	}
}

func setupKcp(conn *kcp.UDPSession) {
	conn.SetStreamMode(false)
	conn.SetWriteDelay(false)
	// 这个参数需要好好研究
	conn.SetNoDelay(1, 10, 2, 1)
	conn.SetMtu(1400)
	conn.SetWindowSize(1, 1)
	conn.SetACKNoDelay(true)
}
