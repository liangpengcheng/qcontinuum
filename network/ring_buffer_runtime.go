package network

import (
	"sync/atomic"
	"unsafe"
)

// Push publishes data only after the producer owns the slot. The per-slot
// sequence makes the queue safe for multiple producers and consumers.
func (rb *RingBuffer) Push(data unsafe.Pointer) bool {
	if data == nil {
		return false
	}

	for {
		pos := rb.head.Load()
		slot := &rb.slots[pos&rb.mask]
		sequence := slot.sequence.Load()
		diff := int64(sequence) - int64(pos)

		switch {
		case diff == 0:
			if rb.head.CompareAndSwap(pos, pos+1) {
				atomic.StorePointer(&slot.data, data)
				slot.sequence.Store(pos + 1)
				return true
			}
		case diff < 0:
			return false
		}
	}
}

func (rb *RingBuffer) Pop() unsafe.Pointer {
	for {
		pos := rb.tail.Load()
		slot := &rb.slots[pos&rb.mask]
		sequence := slot.sequence.Load()
		diff := int64(sequence) - int64(pos+1)

		switch {
		case diff == 0:
			if rb.tail.CompareAndSwap(pos, pos+1) {
				data := atomic.SwapPointer(&slot.data, nil)
				slot.sequence.Store(pos + rb.mask + 1)
				return data
			}
		case diff < 0:
			return nil
		}
	}
}

func (rb *RingBuffer) Size() uint64 {
	head := rb.head.Load()
	tail := rb.tail.Load()
	if head < tail {
		return 0
	}
	return head - tail
}
