package device

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/stretchr/testify/require"
)

// ---- helpers ----

func newWriterTestDev() (*Device, *aoutMockTransport) {
	mt := &aoutMockTransport{}
	return NewDevice(mt, USB1808), mt
}

func defaultWriterCfg() AOutScanConfig {
	return AOutScanConfig{
		Channels: []int{0},
		Rate:     10000,
		Count:    0, // continuous
	}
}

// stallRing is a BulkOutRing that never calls source.Fill.
// It is used to make Write block (since pending is never read).
type stallRing struct {
	done   chan struct{}
	stopCh chan struct{}
	once   sync.Once
}

func (r *stallRing) Done() <-chan struct{} { return r.done }
func (r *stallRing) Err() error           { return nil }
func (r *stallRing) Stop() {
	r.once.Do(func() {
		close(r.stopCh)
		close(r.done)
	})
}
func (r *stallRing) Stats() transport.BulkOutRingStats { return transport.BulkOutRingStats{} }

// stallTransport embeds aoutMockTransport but returns a stallRing that never
// reads from the source, so Write blocks indefinitely.
type stallTransport struct {
	aoutMockTransport
}

func (m *stallTransport) NewBulkOutRing(_ uint8, _, _ int, _ transport.BulkOutSource, _ uint) (transport.BulkOutRing, error) {
	return &stallRing{
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}, nil
}

// ---- tests ----

func TestNewAOutWriter_RejectNonZeroCount(t *testing.T) {
	dev, _ := newWriterTestDev()
	_, err := dev.NewAOutWriter(AOutScanConfig{Channels: []int{0}, Rate: 10000, Count: 100})
	require.Error(t, err)
}

func TestAOutWriter_StartWriteClose(t *testing.T) {
	dev, _ := newWriterTestDev()

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	samples := []uint16{0x8000, 0xC000, 0x4000}
	n, err := w.Write(samples)
	require.NoError(t, err)
	require.Equal(t, len(samples), n)

	require.NoError(t, w.Close())
}

func TestAOutWriter_CloseBeforeStart(t *testing.T) {
	dev, _ := newWriterTestDev()

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- w.Close() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Close before Start hung")
	}
}

func TestAOutWriter_WriteAfterClose(t *testing.T) {
	dev, _ := newWriterTestDev()

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	require.NoError(t, w.Close())

	_, err = w.Write([]uint16{0x8000})
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestAOutWriter_AbortUnblocksWrite(t *testing.T) {
	// stallTransport returns a ring that never reads from the source, so
	// Write blocks because the pending channel is never drained.
	mt := &stallTransport{}
	dev := NewDevice(mt, USB1808)

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	writeErr := make(chan error, 1)
	go func() {
		_, err := w.Write([]uint16{0x8000, 0xC000})
		writeErr <- err
	}()

	time.Sleep(30 * time.Millisecond)
	require.NoError(t, w.Abort())

	select {
	case err := <-writeErr:
		require.Error(t, err, "Write must return an error after Abort")
	case <-time.After(2 * time.Second):
		t.Fatal("Write did not unblock after Abort")
	}

	// Subsequent Write calls must also fail.
	_, err = w.Write([]uint16{0x8000})
	require.Error(t, err)
}

func TestAOutWriter_Abort(t *testing.T) {
	dev, _ := newWriterTestDev()

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	_, err = w.Write([]uint16{0x8000, 0xC000})
	require.NoError(t, err)

	require.NoError(t, w.Abort())

	_, err = w.Write([]uint16{0x8000})
	require.Error(t, err)
}

func TestAOutWriter_ConcurrentWritesSerialized(t *testing.T) {
	dev, _ := newWriterTestDev()

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	const goroutines = 4
	const writesEach = 8
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			samples := make([]uint16, 4)
			for range writesEach {
				if _, e := w.Write(samples); e != nil {
					errs[idx] = e
					return
				}
			}
		}(i)
	}

	wg.Wait()
	require.NoError(t, w.Close())
	for i, e := range errs {
		require.NoError(t, e, "goroutine %d error", i)
	}
}

func TestAOutWriter_EmptyWrite(t *testing.T) {
	dev, _ := newWriterTestDev()

	w, err := dev.NewAOutWriter(defaultWriterCfg())
	require.NoError(t, err)
	require.NoError(t, w.Start())

	n, err := w.Write(nil)
	require.NoError(t, err)
	require.Equal(t, 0, n)

	n, err = w.Write([]uint16{})
	require.NoError(t, err)
	require.Equal(t, 0, n)

	require.NoError(t, w.Close())
}

func TestWriterSource_Fill_Leftover(t *testing.T) {
	stop := make(chan struct{})
	done := make(chan struct{})
	src := &writerSource{
		pending: make(chan []uint16),
		done:    done,
		stop:    stop,
	}

	// Send a 3-sample chunk; buf only fits 2.
	go func() {
		src.pending <- []uint16{0xAAAA, 0x5555, 0x1234}
	}()

	buf := make([]byte, 4) // fits 2 uint16s
	n, err := src.Fill(buf)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Len(t, src.leftover, 1)
	require.Equal(t, uint16(0x1234), src.leftover[0])

	// Drain leftover on next Fill call.
	n, err = src.Fill(buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Empty(t, src.leftover)

	// Close done → EOF.
	close(done)
	n, err = src.Fill(buf)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 0, n)
}

func TestWriterSource_Fill_StopUnblocks(t *testing.T) {
	stop := make(chan struct{})
	done := make(chan struct{})
	src := &writerSource{
		pending: make(chan []uint16),
		done:    done,
		stop:    stop,
	}

	// Close stop → Fill must return io.EOF immediately.
	close(stop)
	buf := make([]byte, 4)
	n, err := src.Fill(buf)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 0, n)
}
