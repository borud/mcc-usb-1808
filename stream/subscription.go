package stream

import (
	"sync"
	"sync/atomic"
)

// Subscription receives copies of chunks broadcast by a Fanout.
type Subscription struct {
	ch      chan []byte
	pool    sync.Pool
	dropped atomic.Uint64
	err     error
	mu      sync.Mutex
}

func newSubscription(depth int) *Subscription {
	return &Subscription{
		ch: make(chan []byte, depth),
		pool: sync.Pool{
			New: func() any { return nil },
		},
	}
}

// Chunks returns the channel delivering chunk copies.
func (s *Subscription) Chunks() <-chan []byte {
	return s.ch
}

// Release returns a buffer to the subscription's pool for reuse.
// Call this after you are done processing a chunk.
func (s *Subscription) Release(buf []byte) {
	if buf != nil {
		s.pool.Put(buf) //nolint:staticcheck
	}
}

// Err returns any error set when the source closed with an error.
func (s *Subscription) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Dropped returns the total number of chunks dropped due to a full buffer.
func (s *Subscription) Dropped() uint64 {
	return s.dropped.Load()
}

func (s *Subscription) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}
