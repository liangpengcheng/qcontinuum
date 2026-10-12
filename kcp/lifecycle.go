package kcp

import "sync/atomic"

func (s *AsyncKCPServer) initiateStop() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.stopOnce.Do(func() {
		atomic.StoreInt32(&s.running, 0)
		close(s.stopChan)
		if s.listener != nil {
			_ = s.listener.Close()
		}
	})
}

func (s *AsyncKCPServer) Stop() {
	s.initiateStop()
	// No connection handler may be added once the accept loop has exited.
	s.acceptWG.Wait()
	s.peers.Range(func(key, _ interface{}) bool {
		key.(*KCPPeer).Close()
		return true
	})
	s.wg.Wait()
}
