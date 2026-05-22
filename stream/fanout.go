package stream

import (
	"sync"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// Fanout broadcasts chunks from a source channel to multiple subscribers.
// Each subscriber receives independent copies of the data.
type Fanout struct {
	src  <-chan device.Chunk
	subs []*Subscription
	mu   sync.Mutex
	done chan struct{}
	err  error
}

// NewFanout creates a Fanout that reads from src.
// Call Subscribe to add consumers, then Run to start broadcasting.
func NewFanout(src <-chan device.Chunk) *Fanout {
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
				buf = *pooled.(*[]byte)
				if cap(buf) >= len(chunk.Data) {
					buf = buf[:len(chunk.Data)]
				} else {
					buf = make([]byte, len(chunk.Data))
				}
			} else {
				buf = make([]byte, len(chunk.Data))
			}
			copy(buf, chunk.Data)

			cp := device.Chunk{
				Data:       buf,
				FrameStart: chunk.FrameStart,
				FrameCount: chunk.FrameCount,
			}
			select {
			case sub.ch <- cp:
			default:
				sub.pool.Put(&buf)
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

// AwaitDone blocks until Run exits. Run exits when the source channel is
// closed by the producer; this method does not signal Run to stop.
func (f *Fanout) AwaitDone() {
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
