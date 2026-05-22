package device

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

// hangingTransport simulates a USB transport where BulkReadInto blocks
// forever after all data has been delivered — reproducing the macOS
// behaviour that caused finite scans to hang.  BulkReadInto unblocks
// when cmdAInBulkFlush is sent via ControlOut (as the watchdog does).
type hangingTransport struct {
	mu        sync.Mutex
	frames    []byte // remaining frame data to deliver
	frameSize int
	flushed   chan struct{} // closed by ControlOut(cmdAInBulkFlush)
	flushOnce sync.Once
	reads     atomic.Int64
}

func newHangingTransport(frameData []byte, frameSize int) *hangingTransport {
	return &hangingTransport{
		frames:    append([]byte(nil), frameData...),
		frameSize: frameSize,
		flushed:   make(chan struct{}),
	}
}

func (h *hangingTransport) ControlOut(request uint8, _, _ uint16, _ []byte) error {
	if request == cmdAInBulkFlush {
		h.flushOnce.Do(func() { close(h.flushed) })
	}
	return nil
}

func (h *hangingTransport) ControlIn(_ uint8, _, _ uint16, n int) ([]byte, error) {
	return make([]byte, n), nil
}

func (h *hangingTransport) BulkRead(_ uint8, _ int, _ time.Duration) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (h *hangingTransport) BulkReadInto(_ uint8, buf []byte, _ time.Duration) (int, error) {
	h.reads.Add(1)

	h.mu.Lock()
	remaining := len(h.frames)
	h.mu.Unlock()

	if remaining > 0 {
		h.mu.Lock()
		n := copy(buf, h.frames)
		h.frames = h.frames[n:]
		h.mu.Unlock()
		return n, nil
	}

	// No data left — block until flush (simulates stuck macOS bulk read).
	<-h.flushed
	return 0, fmt.Errorf("flushed")
}

func (h *hangingTransport) BulkWrite(_ uint8, data []byte, _ time.Duration) (int, error) {
	return len(data), nil
}

func (h *hangingTransport) Close() error { return nil }

func TestCreateScan_ConfigSendsCorrectTransfers(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ADC setup
		transport.MockResponse{}, // scan queue config
	)
	dev := NewDevice(mock, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP5V, Mode: SingleEnded},
			{Index: ScanChanDIO, Type: ChannelTypeDIO},
		},
		Rate: 100000,
	}
	h, err := dev.CreateScan(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify ADC setup was sent.
	adcCall := mock.Calls[0]
	if adcCall.Request != cmdADCSetup {
		t.Errorf("first call request = 0x%02x, want 0x%02x", adcCall.Request, cmdADCSetup)
	}
	// Ch0: BP10V|Diff = 0x00, Ch1: BP5V|(SE<<2) = 0x05
	if adcCall.Data[0] != 0x00 {
		t.Errorf("ADC ch0 = 0x%02x, want 0x00", adcCall.Data[0])
	}
	if adcCall.Data[1] != 0x05 {
		t.Errorf("ADC ch1 = 0x%02x, want 0x05", adcCall.Data[1])
	}

	// Verify scan queue config.
	queueCall := mock.Calls[1]
	if queueCall.Request != cmdAInConfig {
		t.Errorf("second call request = 0x%02x, want 0x%02x", queueCall.Request, cmdAInConfig)
	}
	if queueCall.WIndex != 2 { // lastChan - 1 = 3 channels - 1 = 2
		t.Errorf("wIndex = %d, want 2", queueCall.WIndex)
	}
	if queueCall.Data[0] != 0 || queueCall.Data[1] != 1 || queueCall.Data[2] != ScanChanDIO {
		t.Errorf("queue = %v, want [0 1 8]", queueCall.Data[:3])
	}

	// Verify handle properties.
	if h.Rate() != 100000 {
		t.Errorf("rate = %d, want 100000", h.Rate())
	}
	if h.FrameSize() != 3*4 {
		t.Errorf("frameSize = %d, want %d", h.FrameSize(), 3*4)
	}
	if len(h.Channels()) != 3 {
		t.Errorf("channels = %d, want 3", len(h.Channels()))
	}
}

func TestCreateScan_ValidationErrors(t *testing.T) {
	mock := transport.NewMockTransport()
	dev := NewDevice(mock, USB1808)

	// Empty channels.
	_, err := dev.CreateScan(ScanConfig{Channels: nil, Rate: 1000})
	if err == nil {
		t.Error("expected error for empty channels")
	}

	// Invalid rate.
	_, err = dev.CreateScan(ScanConfig{
		Channels: []ChannelConfig{{Index: 0, Type: ChannelTypeAnalog}},
		Rate:     0,
	})
	if err == nil {
		t.Error("expected error for zero rate")
	}

	// Invalid channel index.
	_, err = dev.CreateScan(ScanConfig{
		Channels: []ChannelConfig{{Index: 99, Type: ChannelTypeAnalog}},
		Rate:     1000,
	})
	if err == nil {
		t.Error("expected error for invalid channel index")
	}
}

func TestScanHandle_StartAndStop(t *testing.T) {
	// Build mock that handles: ADC setup, queue config, clear FIFO, scan start,
	// then bulk reads that return data, then stop/flush commands.
	nCh := 2
	frameSize := nCh * 4
	// Create one frame of data.
	frameData := make([]byte, frameSize)
	copy(frameData[0:4], wire.PutUint32LE(131072)) // midscale
	copy(frameData[4:8], wire.PutUint32LE(262143)) // max

	mock := transport.NewMockTransport(
		transport.MockResponse{},                // ADC setup
		transport.MockResponse{},                // scan queue config
		transport.MockResponse{},                // clear FIFO
		transport.MockResponse{},                // scan start
		transport.MockResponse{Data: frameData}, // first bulk read
		transport.MockResponse{Data: frameData}, // second bulk read
		transport.MockResponse{Data: frameData}, // third bulk read
		transport.MockResponse{Data: frameData}, // fourth bulk read — enough for readers
		// Stop will trigger errors on subsequent reads, causing readers to exit.
	)
	dev := NewDevice(mock, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
		},
		Rate:  1000,
		Count: 2,
	}
	h, err := dev.CreateScan(cfg, WithPipelineDepth(4), WithConcurrentReaders(1))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Start(); err != nil {
		t.Fatal(err)
	}

	// Read at least one chunk.
	chunk, ok := <-h.Chunks()
	if !ok {
		t.Fatal("chunks channel closed without data")
	}
	if len(chunk.Data) < frameSize {
		t.Errorf("chunk size = %d, want >= %d", len(chunk.Data), frameSize)
	}

	// Channel should close when count is reached or we stop.
	// Drain remaining.
	for range h.Chunks() {
		_ = true // drain
	}

	if h.Err() != nil {
		t.Errorf("unexpected error: %v", h.Err())
	}
}

func TestScanHandle_WatchdogUnblocksHangingReaders(t *testing.T) {
	// Simulate a finite scan where BulkReadInto blocks forever after
	// the device finishes sending data (the macOS hang).  The watchdog
	// must send BulkFlush to unblock the readers so Chunks() closes.
	const (
		nCh       = 2
		frameSize = nCh * 4
		count     = 10
		rate      = 100000 // high rate → short expected duration → fast watchdog
	)

	// Build frame data for exactly `count` frames.
	allFrames := make([]byte, count*frameSize)
	for i := range count {
		copy(allFrames[i*frameSize:], wire.PutUint32LE(131072))
		copy(allFrames[i*frameSize+4:], wire.PutUint32LE(131072))
	}

	ht := newHangingTransport(allFrames, frameSize)
	dev := NewDevice(ht, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
		},
		Rate:  rate,
		Count: count,
	}
	h, err := dev.CreateScan(cfg, WithPipelineDepth(4), WithConcurrentReaders(1))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Start(); err != nil {
		t.Fatal(err)
	}

	// Drain Chunks() — without the watchdog this would hang forever.
	done := make(chan int)
	go func() {
		total := 0
		for chunk := range h.Chunks() {
			total += chunk.FrameCount
		}
		done <- total
	}()

	select {
	case got := <-done:
		if got < count {
			t.Errorf("received %d frames, want >= %d", got, count)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Chunks() did not close — watchdog failed to unblock readers")
	}
}
