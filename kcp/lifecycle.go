package kcp

import "sync/atomic"

func (s *AsyncKCPServer) Stop() {
	s.stopOnce.Do(func() {
		atomic.StoreInt32(&s.running, 0)
		close(s.stopChan)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.peers.Range(func(key, _ interface{}) bool {
			key.(*KCPPeer).Close()
			return true
		})
		s.wg.Wait()
	})
}
