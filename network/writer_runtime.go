package network

import (
	"errors"
	"sync/atomic"
	"unsafe"
)

const maxWriteQueueBytes int64 = 128 * 1024 * 1024

func (w *ZeroCopyMessageWriter) configure(
	onWriteInterest func(bool) error,
	onError func(error),
	onBytes func(int),
) {
	w.onWriteInterest = onWriteInterest
	w.onError = onError
	w.onBytes = onBytes
}

func (w *ZeroCopyMessageWriter) enqueue(req *writeRequest) bool {
	if req == nil || req.buffer == nil {
		return false
	}

	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if atomic.LoadInt32(&w.closed) != 0 {
		return false
	}

	size := int64(req.buffer.Len())
	for {
		current := atomic.LoadInt64(&w.queuedBytes)
		if size > maxWriteQueueBytes-current {
			return false
		}
		if atomic.CompareAndSwapInt64(&w.queuedBytes, current, current+size) {
			break
		}
	}
	atomic.StoreInt32(&req.accounted, 1)

	if !w.writeQueue.Push(unsafe.Pointer(req)) {
		atomic.StoreInt32(&req.accounted, 0)
		atomic.AddInt64(&w.queuedBytes, -size)
		return false
	}
	return true
}

func (w *ZeroCopyMessageWriter) releaseRequest(req *writeRequest) {
	if req == nil || !atomic.CompareAndSwapInt32(&req.released, 0, 1) {
		return
	}
	if req.buffer != nil {
		size := int64(req.buffer.Len())
		req.buffer.Release()
		req.buffer = nil
		if atomic.CompareAndSwapInt32(&req.accounted, 1, 0) {
			atomic.AddInt64(&w.queuedBytes, -size)
		}
	}
}

func (w *ZeroCopyMessageWriter) takePending() *writeRequest {
	w.pendingMu.Lock()
	req := w.pending
	w.pending = nil
	w.pendingMu.Unlock()
	return req
}

func (w *ZeroCopyMessageWriter) setPending(req *writeRequest) {
	w.pendingMu.Lock()
	w.pending = req
	w.pendingMu.Unlock()
}

func (w *ZeroCopyMessageWriter) hasWork() bool {
	w.pendingMu.Lock()
	hasPending := w.pending != nil
	w.pendingMu.Unlock()
	return hasPending || w.writeQueue.Size() != 0
}

func (w *ZeroCopyMessageWriter) tryAsyncWrite() {
	w.lifecycleMu.Lock()
	if atomic.LoadInt32(&w.closed) != 0 ||
		!atomic.CompareAndSwapInt32(&w.writing, 0, 1) {
		w.lifecycleMu.Unlock()
		return
	}
	w.wg.Add(1)
	w.lifecycleMu.Unlock()

	go w.drainWrites()
}

func (w *ZeroCopyMessageWriter) drainWrites() {
	defer w.wg.Done()

	for {
		if atomic.LoadInt32(&w.closed) != 0 {
			atomic.StoreInt32(&w.writing, 0)
			return
		}

		req := w.takePending()
		if req == nil {
			if ptr := w.writeQueue.Pop(); ptr != nil {
				req = (*writeRequest)(ptr)
			}
		}

		if req == nil {
			atomic.StoreInt32(&w.writing, 0)
			if !w.hasWork() ||
				!atomic.CompareAndSwapInt32(&w.writing, 0, 1) {
				return
			}
			continue
		}

		blocked, err := w.doWrite(req)
		if err != nil {
			w.releaseRequest(req)
			atomic.StoreInt32(&w.writing, 0)
			if w.onError != nil && atomic.LoadInt32(&w.closed) == 0 {
				go w.onError(err)
			}
			return
		}
		if blocked {
			w.setPending(req)
			atomic.StoreInt32(&w.writing, 0)
			if w.onWriteInterest == nil {
				w.releaseRequest(w.takePending())
				if w.onError != nil {
					go w.onError(errors.New("write blocked without writable-event support"))
				}
				return
			}
			if err := w.onWriteInterest(true); err != nil {
				w.releaseRequest(w.takePending())
				if w.onError != nil {
					go w.onError(err)
				}
			}
			return
		}

		w.releaseRequest(req)
	}
}

func (w *ZeroCopyMessageWriter) notifyWritable() error {
	if w.onWriteInterest != nil {
		if err := w.onWriteInterest(false); err != nil {
			return err
		}
	}
	w.tryAsyncWrite()
	return nil
}

func (w *ZeroCopyMessageWriter) Close() {
	w.lifecycleMu.Lock()
	atomic.StoreInt32(&w.closed, 1)
	w.lifecycleMu.Unlock()

	if w.onWriteInterest != nil {
		_ = w.onWriteInterest(false)
	}
	w.wg.Wait()

	w.releaseRequest(w.takePending())
	for {
		ptr := w.writeQueue.Pop()
		if ptr == nil {
			break
		}
		w.releaseRequest((*writeRequest)(ptr))
	}
}
