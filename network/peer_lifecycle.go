package network

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/liangpengcheng/qcontinuum/base"
)

func (w *ZeroCopyMessageWriter) beginClose() {
	w.lifecycleMu.Lock()
	atomic.StoreInt32(&w.closed, 1)
	w.lifecycleMu.Unlock()
	if w.onWriteInterest != nil {
		_ = w.onWriteInterest(false)

	}
}
func (peer *AsyncClientPeer) CheckAfter(timeout time.Duration) {
	time.AfterFunc(timeout, func() {
		if peer.GetState() != PeerStateConnected || atomic.LoadInt64(&peer.ID) != 0 {
			return
		}
		proc := peer.getProcessor()
		if proc == nil {
			peer.Close()
			return
		}
		select {
		case proc.FuncChan <- func() {
			if peer.GetState() == PeerStateConnected && atomic.LoadInt64(&peer.ID) == 0 {
				peer.Close()
			}
		}:
		default:
			peer.Close()
		}
	})
}

func (peer *AsyncClientPeer) startReactorIO() error {
	peer.lifecycleMu.Lock()
	defer peer.lifecycleMu.Unlock()
	if peer.GetState() != PeerStateConnected {
		return errors.New("peer not connected")
	}
	if peer.reactor == nil || peer.FD < 0 {
		return errors.New("reactor or fd is not available")
	}

	peer.writer.configure(
		func(enabled bool) error {
			events := EpollIn | EpollET
			if enabled {
				events |= EpollOut
			}
			return peer.reactor.ModifyFd(peer.FD, events)
		},
		func(err error) {
			peer.OnError(peer.FD, err)
		},
		func(n int) {
			atomic.AddUint64(&peer.bytesWritten, uint64(n))
		},
	)

	return peer.reactor.AddFd(peer.FD, EpollIn|EpollET, peer)
}

func (peer *AsyncClientPeer) Close() {
	peer.closeWithEvent(nil)
}

// A non-nil event requests explicit removal and must be admitted before closing.
// Admission and the closing transition share lifecycleMu so Redirect and message
// delivery cannot change ownership or start work between them.
func (peer *AsyncClientPeer) closeWithEvent(event *Event) bool {
	peer.lifecycleMu.Lock()
	proc := peer.getProcessorLocked()
	if event != nil && (proc == nil || !proc.postLifecycleEventLocked(event)) {
		peer.lifecycleMu.Unlock()
		return false
	}
	if !atomic.CompareAndSwapInt32(
		&peer.state,
		int32(PeerStateConnected),
		int32(PeerStateClosing),
	) {
		peer.lifecycleMu.Unlock()
		return event != nil // Duplicate explicit removals are acknowledged.
	}
	// Snapshot the owner at the closing transition; Redirect cannot change it now.
	// Publish before another Close can observe Closing and return. The consumer
	// waits for transport cleanup and all in-flight callbacks before removal.
	if proc != nil && event == nil {
		proc.postLifecycleEventLocked(&Event{ID: RemoveEvent, Peer: peer.ToClientPeer()})
	}

	if peer.writer != nil {
		peer.writer.beginClose()
	}
	if peer.reactor != nil && peer.FD >= 0 {
		_ = peer.reactor.RemoveFd(peer.FD)
	}
	if peer.Connection != nil {
		_ = peer.Connection.Close()
	}
	if peer.FD >= 0 {
		removeConnFromFd(peer.FD)
	}
	peer.lifecycleMu.Unlock()

	if peer.writer != nil {
		peer.writer.Close()
	}
	if peer.reader != nil {
		peer.reader.Release()
	}

	atomic.StoreInt32(&peer.state, int32(PeerStateClosed))

	// Wake the recorded removal before transport completion signals server Stop.
	if proc != nil {
		proc.signalLifecycle()
	}
	if peer.onClosed != nil {
		peer.onClosed()
	}
	if peer.ownsReactor && peer.reactor != nil {
		peer.reactor.Stop()
	}
	return true
}

func (peer *AsyncClientPeer) OnError(fd int, err error) {
	if peer.GetState() != PeerStateConnected {
		return
	}
	base.Zap().Sugar().Warnf("peer error on fd %d: %v", fd, err)
	peer.Close()
}

func (peer *AsyncClientPeer) OnClose(fd int) {
	if peer.GetState() != PeerStateConnected {
		return
	}
	peer.Close()
}
