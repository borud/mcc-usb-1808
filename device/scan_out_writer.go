package device

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/borud/mcc-usb-1808/v4/wire"
)

// AOutWriter is a streaming analog output writer. It wraps an AOutScanHandle
// and exposes a Write-based API with backpressure flowing from the device FIFO
// through the bulk-OUT ring to the calling goroutine. Only continuous mode
// (cfg.Count == 0) is supported.
//
// Typical usage:
//
//	w, err := dev.NewAOutWriter(cfg)
//	if err != nil { ... }
//	defer w.Close()
//	if err := w.Start(); err != nil { ... }
//	for _, chunk := range chunks {
//	    if _, err := w.Write(chunk); err != nil { break }
//	}
type AOutWriter struct {
	h    *AOutScanHandle
	src  *writerSource
	once sync.Once
}

// NewAOutWriter creates a continuous analog output scan driven by caller-
// supplied Write calls. cfg.Count must be 0 (continuous mode).
func (d *Device) NewAOutWriter(cfg AOutScanConfig, opts ...AOutScanOption) (*AOutWriter, error) {
	if cfg.Count != 0 {
		return nil, fmt.Errorf("usb1808: AOutWriter requires continuous mode (Count=0)")
	}
	h, err := d.CreateAOutScan(cfg, opts...)
	if err != nil {
		return nil, err
	}
	src := &writerSource{
		pending: make(chan []uint16),
		done:    make(chan struct{}),
		stop:    h.stop,
	}
	h.src = src
	return &AOutWriter{h: h, src: src}, nil
}

// Start arms the scan. The device does not begin output until the bulk-OUT ring
// has received the first URB from Write calls.
func (w *AOutWriter) Start() error { return w.h.Start() }

// Write encodes samples as little-endian uint16 pairs and streams them to the
// device. It blocks under backpressure while no ring slot is free. Returns
// io.ErrClosedPipe after Close or Abort. Concurrent Write calls are serialized.
func (w *AOutWriter) Write(samples []uint16) (int, error) {
	if len(samples) == 0 {
		return 0, nil
	}
	w.src.mu.Lock()
	defer w.src.mu.Unlock()

	if w.src.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	select {
	case w.src.pending <- samples:
		return len(samples), nil
	case <-w.src.done:
		return 0, io.ErrClosedPipe
	case <-w.h.stop:
		return 0, io.ErrClosedPipe
	}
}

// Close signals end-of-stream. Pending URBs drain to the device; the scan stops
// when the device FIFO empties. Close blocks until draining completes or a
// watchdog fires (rate-derived timeout + 500 ms grace). Idempotent.
func (w *AOutWriter) Close() error {
	w.once.Do(func() { w.src.close() })

	w.h.mu.Lock()
	started := w.h.state != scanIdle
	w.h.mu.Unlock()
	if !started {
		return nil
	}

	watchdog := time.NewTimer(w.drainTimeout())
	defer watchdog.Stop()

	select {
	case <-w.h.done:
	case <-watchdog.C:
		w.h.stopOnce.Do(func() { close(w.h.stop) })
		<-w.h.done
	}
	return w.h.Err()
}

// Abort cancels in-flight transfers and returns immediately. Hardware stop and
// resource cleanup happen asynchronously. Safe to call concurrently with Write.
func (w *AOutWriter) Abort() error {
	w.src.close()
	w.h.mu.Lock()
	if w.h.state != scanRunning {
		w.h.mu.Unlock()
		return nil
	}
	w.h.state = scanStopped
	w.h.mu.Unlock()
	w.h.stopOnce.Do(func() { close(w.h.stop) })
	return nil
}

// Err returns any error that occurred during writing.
func (w *AOutWriter) Err() error { return w.h.Err() }

// drainTimeout returns the watchdog duration for Close. It is computed from the
// maximum bytes in-flight divided by the playback rate, plus 500 ms grace.
func (w *AOutWriter) drainTimeout() time.Duration {
	if w.h.bufSize <= 0 || w.h.cfg.Rate <= 0 || w.h.frameSize <= 0 {
		return 5 * time.Second
	}
	maxInFlight := aoutRingTransferCount * w.h.bufSize
	drainSec := float64(maxInFlight) / float64(w.h.cfg.Rate*w.h.frameSize)
	return time.Duration(drainSec*float64(time.Second)) + 500*time.Millisecond
}

// writerSource is a samplesSource fed by AOutWriter.Write. Fill blocks on an
// unbuffered channel until Write provides the next chunk, the writer is closed,
// or the scan is aborted.
type writerSource struct {
	// pending carries []uint16 chunks from Write to Fill (unbuffered; never
	// closed — closing done signals EOF instead to avoid send-on-closed panic).
	pending chan []uint16
	// leftover holds samples from a chunk that did not fit in the last Fill buf.
	leftover []uint16
	// done is closed by close() to signal end-of-stream to Fill.
	done chan struct{}
	// stop is a reference to AOutScanHandle.stop; when closed, Fill returns
	// io.EOF so the ring aborts cleanly without waiting for a new Write.
	stop <-chan struct{}

	mu        sync.Mutex  // serializes Write calls
	closeOnce sync.Once
	closed    atomic.Bool
}

func (s *writerSource) close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.done)
	})
}

// Fill implements samplesSource. Called exclusively by the bulk-OUT ring's
// refill goroutine; no concurrent callers.
func (s *writerSource) Fill(buf []byte) (int, error) {
	// Drain leftover from a chunk that did not fit in the previous buf.
	if len(s.leftover) > 0 {
		n := wire.Uint16sToBytesInto(buf, s.leftover)
		s.leftover = s.leftover[n/2:]
		return n, nil
	}
	// Block until the next chunk arrives, the writer is closed, or the ring
	// is aborted via the scan stop channel.
	select {
	case chunk := <-s.pending:
		n := wire.Uint16sToBytesInto(buf, chunk)
		if n/2 < len(chunk) {
			s.leftover = chunk[n/2:]
		}
		return n, nil
	case <-s.done:
		return 0, io.EOF
	case <-s.stop:
		return 0, io.EOF
	}
}
