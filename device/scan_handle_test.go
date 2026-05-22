package device

import (
	"testing"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

// mockRingTransport implements both Transport and AsyncBulkReader for testing.
// It records control calls and returns a mock BulkRing that delivers
// pre-configured results.
type mockRingTransport struct {
	calls   []transport.Call
	results []transport.RingResult
}

func (m *mockRingTransport) ControlOut(request uint8, wValue, wIndex uint16, data []byte) error {
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.calls = append(m.calls, transport.Call{
		Method: "ControlOut", Request: request, WValue: wValue, WIndex: wIndex, Data: dataCopy,
	})
	return nil
}

func (m *mockRingTransport) ControlIn(_ uint8, _, _ uint16, n int) ([]byte, error) {
	return make([]byte, n), nil
}

func (m *mockRingTransport) BulkRead(_ uint8, _ int, _ time.Duration) ([]byte, error) {
	return nil, nil
}

func (m *mockRingTransport) BulkReadInto(_ uint8, _ []byte, _ time.Duration) (int, error) {
	return 0, nil
}

func (m *mockRingTransport) BulkWrite(_ uint8, data []byte, _ time.Duration) (int, error) {
	return len(data), nil
}

func (m *mockRingTransport) Close() error { return nil }

func (m *mockRingTransport) NewBulkRing(_ uint8, _, _, _ int, _ uint) (transport.BulkRing, error) {
	ch := make(chan transport.RingResult, len(m.results))
	for _, r := range m.results {
		ch <- r
	}
	close(ch)
	return &mockBulkRing{ch: ch}, nil
}

// mockBulkRing implements transport.BulkRing for tests.
type mockBulkRing struct {
	ch      chan transport.RingResult
	stopped bool
}

func (r *mockBulkRing) Results() <-chan transport.RingResult {
	return r.ch
}

func (r *mockBulkRing) Stop() {
	r.stopped = true
}

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
	const (
		nCh       = 2
		frameSize = nCh * 4
	)

	frameData := make([]byte, frameSize)
	copy(frameData[0:4], wire.PutUint32LE(131072))
	copy(frameData[4:8], wire.PutUint32LE(262143))

	mt := &mockRingTransport{
		results: []transport.RingResult{
			{Data: frameData},
			{Data: frameData},
		},
	}
	dev := NewDevice(mt, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
		},
		Rate:  1000,
		Count: 2,
	}
	h, err := dev.CreateScan(cfg, WithPipelineDepth(4))
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

	// Drain remaining.
	for range h.Chunks() {
		_ = true // drain
	}

	if h.Err() != nil {
		t.Errorf("unexpected error: %v", h.Err())
	}
}

func TestScanHandle_FiniteScanCompletesViaRing(t *testing.T) {
	const (
		nCh       = 2
		frameSize = nCh * 4
		count     = 10
	)

	// Build frame data for exactly `count` frames.
	allFrames := make([]byte, count*frameSize)
	for i := range count {
		copy(allFrames[i*frameSize:], wire.PutUint32LE(131072))
		copy(allFrames[i*frameSize+4:], wire.PutUint32LE(131072))
	}

	mt := &mockRingTransport{
		results: []transport.RingResult{
			{Data: allFrames},
		},
	}
	dev := NewDevice(mt, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
		},
		Rate:  100000,
		Count: count,
	}
	h, err := dev.CreateScan(cfg, WithPipelineDepth(4))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Start(); err != nil {
		t.Fatal(err)
	}

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
		if got != count {
			t.Errorf("received %d frames, want %d", got, count)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Chunks() did not close in time")
	}
}
