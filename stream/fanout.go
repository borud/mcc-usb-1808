// Package stream provides fan-out broadcasting of raw scan chunks
// to multiple concurrent consumers.
package stream

import "sync"

// Fanout broadcasts chunks from a source channel to multiple subscribers.
// Each subscriber receives independent copies of the data.
type Fanout struct {
	src  <-chan []byte
	subs []*Subscription
	mu   sync.Mutex
	done chan struct{}
	err  error
}

// NewFanout creates a Fanout that reads from src.
// Call Subscribe to add consumers, then Run to start broadcasting.
func NewFanout(src <-chan []byte) *Fanout {
	return &Fanout{
		src:  src,
		done: make(chan struct{}),
	}
}

// Subscribe adds a subscriber and returns the subscription handle.
// Must be called before Run.
func (f *Fanout) Subscribe(opts ...SubscribeOption) *Subscription {
	o := subscribeOptions{depth: defaultSubscriptionDepth}
	for _, opt := range opts {
		opt(&o)
	}
	sub := newSubscription(o.depth)
	f.mu.Lock()
	f.subs = append(f.subs, sub)
	f.mu.Unlock()
	return sub
}

// Run reads from the source and broadcasts to all subscribers.
// It blocks until the source channel is closed or Stop is called.
func (f *Fanout) Run() {
	defer close(f.done)
	defer f.closeAll()

	for chunk := range f.src {
		f.mu.Lock()
		subs := f.subs
		f.mu.Unlock()

		for _, sub := range subs {
			// Get a buffer from the subscription's pool or allocate.
			var buf []byte
			if pooled := sub.pool.Get(); pooled != nil {
				buf = pooled.([]byte)
				if cap(buf) >= len(chunk) {
					buf = buf[:len(chunk)]
				} else {
					buf = make([]byte, len(chunk))
				}
			} else {
				buf = make([]byte, len(chunk))
			}
			copy(buf, chunk)

			select {
			case sub.ch <- buf:
			default:
				sub.pool.Put(buf) //nolint:staticcheck
				sub.dropped.Add(1)
			}
		}
	}
}

// SetErr sets an error that will be propagated to all subscribers.
// Call before the source channel closes to communicate upstream errors.
func (f *Fanout) SetErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

// Stop signals all subscribers that broadcast has ended.
// It does not close the source channel (the producer owns that).
func (f *Fanout) Stop() {
	// Wait for Run to finish (source must close for Run to exit).
	<-f.done
}

// Done returns a channel that is closed when Run exits.
func (f *Fanout) Done() <-chan struct{} {
	return f.done
}

func (f *Fanout) closeAll() {
	f.mu.Lock()
	err := f.err
	f.mu.Unlock()

	for _, sub := range f.subs {
		sub.setErr(err)
		close(sub.ch)
	}
}
