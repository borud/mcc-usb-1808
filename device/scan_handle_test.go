package device

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
	"github.com/stretchr/testify/require"
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

// overrunMockTransport simulates an overrun: the first ring delivers some
// data then an error. ControlIn returns the overrun status bit.
// After recovery, subsequent rings deliver more data normally.
type overrunMockTransport struct {
	calls    []transport.Call
	mu       sync.Mutex
	ringCall int // tracks NewBulkRing calls
	// rings[0] = results for first ring (ends with error)
	// rings[1] = results for second ring (after recovery)
	rings [][]transport.RingResult
}

func (m *overrunMockTransport) ControlOut(request uint8, wValue, wIndex uint16, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.calls = append(m.calls, transport.Call{
		Method: "ControlOut", Request: request, WValue: wValue, WIndex: wIndex, Data: dataCopy,
	})
	return nil
}

func (m *overrunMockTransport) ControlIn(_ uint8, _, _ uint16, n int) ([]byte, error) {
	// Return status with AInScanOverrun bit set.
	buf := make([]byte, n)
	buf[0] = 0x04 // statusAInScanOverrun
	return buf, nil
}

func (m *overrunMockTransport) BulkRead(_ uint8, _ int, _ time.Duration) ([]byte, error) {
	return nil, nil
}
func (m *overrunMockTransport) BulkReadInto(_ uint8, _ []byte, _ time.Duration) (int, error) {
	return 0, nil
}
func (m *overrunMockTransport) BulkWrite(_ uint8, data []byte, _ time.Duration) (int, error) {
	return len(data), nil
}
func (m *overrunMockTransport) Close() error { return nil }

func (m *overrunMockTransport) NewBulkRing(_ uint8, _, _, _ int, _ uint) (transport.BulkRing, error) {
	m.mu.Lock()
	idx := m.ringCall
	m.ringCall++
	m.mu.Unlock()

	if idx >= len(m.rings) {
		// No more data — return empty ring.
		ch := make(chan transport.RingResult)
		close(ch)
		return &mockBulkRing{ch: ch}, nil
	}

	results := m.rings[idx]
	ch := make(chan transport.RingResult, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return &mockBulkRing{ch: ch}, nil
}

func TestScanHandle_OverrunRecovery(t *testing.T) {
	const (
		nCh       = 2
		frameSize = nCh * 4
	)

	frame1 := make([]byte, frameSize)
	copy(frame1[0:4], wire.PutUint32LE(1000))
	copy(frame1[4:8], wire.PutUint32LE(2000))

	frame2 := make([]byte, frameSize)
	copy(frame2[0:4], wire.PutUint32LE(3000))
	copy(frame2[4:8], wire.PutUint32LE(4000))

	frame3 := make([]byte, frameSize)
	copy(frame3[0:4], wire.PutUint32LE(5000))
	copy(frame3[4:8], wire.PutUint32LE(6000))

	mt := &overrunMockTransport{
		rings: [][]transport.RingResult{
			// First ring: one good frame, then error (overrun).
			{
				{Data: frame1},
				{Err: fmt.Errorf("usb: transfer error")},
			},
			// Second ring (after recovery): two more good frames.
			{
				{Data: frame2},
				{Data: frame3},
			},
		},
	}

	dev := NewDevice(mt, USB1808)

	var overrunFrames []uint64
	var overrunMu sync.Mutex

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
		},
		Rate: 1000,
	}
	h, err := dev.CreateScan(cfg,
		WithPipelineDepth(4),
		WithOverrunCallback(func(lastFrame uint64, _ time.Time) {
			overrunMu.Lock()
			overrunFrames = append(overrunFrames, lastFrame)
			overrunMu.Unlock()
		}),
	)
	require.NoError(t, err)
	require.NoError(t, h.Start())

	var chunks []Chunk
	done := make(chan struct{})
	go func() {
		for chunk := range h.Chunks() {
			chunks = append(chunks, chunk)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for chunks")
	}

	require.NoError(t, h.Err())

	// Should have received 3 frames total (1 before overrun + 2 after).
	totalFrames := 0
	for _, c := range chunks {
		totalFrames += c.FrameCount
	}
	require.Equal(t, 3, totalFrames)

	// Overrun callback should have fired once, at frame 1.
	overrunMu.Lock()
	defer overrunMu.Unlock()
	require.Len(t, overrunFrames, 1)
	require.Equal(t, uint64(1), overrunFrames[0])
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
	require.NoError(t, err)

	adcCall := mock.Calls[0]
	require.Equal(t, uint8(cmdADCSetup), adcCall.Request)
	require.Equal(t, uint8(0x00), adcCall.Data[0]) // Ch0: BP10V|Diff
	require.Equal(t, uint8(0x05), adcCall.Data[1]) // Ch1: BP5V|(SE<<2)

	queueCall := mock.Calls[1]
	require.Equal(t, uint8(cmdAInConfig), queueCall.Request)
	require.Equal(t, uint16(2), queueCall.WIndex) // lastChan - 1
	require.Equal(t, uint8(0), queueCall.Data[0])
	require.Equal(t, uint8(1), queueCall.Data[1])
	require.Equal(t, uint8(ScanChanDIO), queueCall.Data[2])

	require.Equal(t, 100000, h.Rate())
	require.Equal(t, 3*4, h.FrameSize())
	require.Len(t, h.Channels(), 3)
}

func TestCreateScan_ValidationErrors(t *testing.T) {
	mock := transport.NewMockTransport()
	dev := NewDevice(mock, USB1808)

	_, err := dev.CreateScan(ScanConfig{Channels: nil, Rate: 1000})
	require.Error(t, err, "expected error for empty channels")

	_, err = dev.CreateScan(ScanConfig{
		Channels: []ChannelConfig{{Index: 0, Type: ChannelTypeAnalog}},
		Rate:     0,
	})
	require.Error(t, err, "expected error for zero rate")

	_, err = dev.CreateScan(ScanConfig{
		Channels: []ChannelConfig{{Index: 99, Type: ChannelTypeAnalog}},
		Rate:     1000,
	})
	require.Error(t, err, "expected error for invalid channel index")
}

func TestScanHandle_StartAndStop(t *testing.T) {
	const (
		nCh       = 2
		frameSize = nCh * 4
	)

	frameData := make([]byte, frameSize)
	copy(frameData[0:4], wire.PutUint32LE(ADCMidscale))
	copy(frameData[4:8], wire.PutUint32LE(ADCFullScale))

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
	require.NoError(t, err)
	require.NoError(t, h.Start())

	chunk, ok := <-h.Chunks()
	require.True(t, ok, "chunks channel closed without data")
	require.GreaterOrEqual(t, len(chunk.Data), frameSize)

	for range h.Chunks() {
		_ = true // drain
	}

	require.NoError(t, h.Err())
}

func TestScanHandle_FiniteScanCompletesViaRing(t *testing.T) {
	const (
		nCh       = 2
		frameSize = nCh * 4
		count     = 10
	)

	allFrames := make([]byte, count*frameSize)
	for i := range count {
		copy(allFrames[i*frameSize:], wire.PutUint32LE(ADCMidscale))
		copy(allFrames[i*frameSize+4:], wire.PutUint32LE(ADCMidscale))
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
	require.NoError(t, err)
	require.NoError(t, h.Start())

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
		require.Equal(t, count, got)
	case <-time.After(5 * time.Second):
		t.Fatal("Chunks() did not close in time")
	}
}
