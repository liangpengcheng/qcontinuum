package network

import (
	"github.com/liangpengcheng/qcontinuum/base"
	kcp "github.com/xtaci/kcp-go"
)

// KcpServer kcp server
type KcpServer struct {
	Listener *kcp.Listener
}
type kcpPeer struct {
	Connection *kcp.UDPSession
}

func (kcp kcpPeer) GetRemoteAddr() string {
	if kcp.Connection != nil {
		return kcp.Connection.RemoteAddr().String()
	}
	return "not connect"
}
func (kcp kcpPeer) Read(buf []byte) (int, error) {
	return kcp.Connection.Read(buf)
}
func (kcp kcpPeer) Write(buf []byte) (int, error) {
	return kcp.Connection.Write(buf)
}
func (kcp kcpPeer) Close() {
	kcp.Connection.Close()
}

func newBlockServer(host string) (*KcpServer, error) {
	crpy, _ := kcp.NewNoneBlockCrypt(nil)
	lis, err := kcp.ListenWithOptions(host, crpy, 10, 2)
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
			base.LogInfo("remote address:", conn.RemoteAddr())
			conn.SetStreamMode(true)
			conn.SetWriteDelay(true)
			// 这个参数需要好好研究
			conn.SetNoDelay(1, 50, 0, 1)
			conn.SetMtu(1400)
			conn.SetWindowSize(2048, 2048)
			conn.SetACKNoDelay(true)
			peer := &ClientPeer{
				Connection: kcpPeer{
					Connection: conn,
				},
			}

			go peer.ConnectionHandler(proc)
		}
	}
}