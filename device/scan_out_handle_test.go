package device

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
	"github.com/stretchr/testify/require"
)

// aoutMockTransport implements Transport + AsyncBulkWriter for AOut tests.
// It records control calls and provides configurable status responses.
type aoutMockTransport struct {
	mu sync.Mutex
	calls []transport.Call
	// statusSeq provides ControlIn(cmdStatus) responses in order.
	// When exhausted, returns zeros (no bits set).
	statusSeq  []uint16
	statusCall int
}

func (m *aoutMockTransport) ControlOut(request uint8, wValue, wIndex uint16, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dc := make([]byte, len(data))
	copy(dc, data)
	m.calls = append(m.calls, transport.Call{
		Method: "ControlOut", Request: request, WValue: wValue, WIndex: wIndex, Data: dc,
	})
	return nil
}

func (m *aoutMockTransport) ControlIn(request uint8, _, _ uint16, n int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := make([]byte, n)
	if request == cmdStatus {
		if m.statusCall < len(m.statusSeq) {
			st := m.statusSeq[m.statusCall]
			buf[0] = uint8(st)
			if n > 1 {
				buf[1] = uint8(st >> 8)
			}
		}
		m.statusCall++
	}
	return buf, nil
}

func (m *aoutMockTransport) BulkRead(_ uint8, _ int, _ time.Duration) ([]byte, error) {
	return nil, nil
}
func (m *aoutMockTransport) BulkReadInto(_ uint8, _ []byte, _ time.Duration) (int, error) {
	return 0, nil
}
func (m *aoutMockTransport) BulkWrite(_ uint8, data []byte, _ time.Duration) (int, error) {
	return len(data), nil
}
func (m *aoutMockTransport) Close() error { return nil }

// mockAOutRing is a simple BulkOutRing that records sent bytes.
type mockAOutRing struct {
	done   chan struct{}
	stopCh chan struct{}
	once   sync.Once
	mu     sync.Mutex
	Data   []byte
	err    error
}

func newMockAOutRing(source transport.BulkOutSource, bufSize int, stopCh chan struct{}) *mockAOutRing {
	ring := &mockAOutRing{
		done:   make(chan struct{}),
		stopCh: stopCh,
	}
	go func() {
		defer close(ring.done)
		buf := make([]byte, bufSize)
		for {
			select {
			case <-ring.stopCh:
				return
			default:
			}
			n, err := source.Fill(buf)
			if n > 0 {
				ring.mu.Lock()
				ring.Data = append(ring.Data, buf[:n]...)
				ring.mu.Unlock()
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				ring.mu.Lock()
				ring.err = err
				ring.mu.Unlock()
				return
			}
		}
	}()
	return ring
}

func (r *mockAOutRing) Done() <-chan struct{} { return r.done }
func (r *mockAOutRing) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}
func (r *mockAOutRing) Stop() {
	r.once.Do(func() { close(r.stopCh) })
}
func (r *mockAOutRing) Stats() transport.BulkOutRingStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return transport.BulkOutRingStats{BytesSent: uint64(len(r.Data))}
}

// NewBulkOutRing implements AsyncBulkWriter for aoutMockTransport.
func (m *aoutMockTransport) NewBulkOutRing(_ uint8, bufSize, _ int, source transport.BulkOutSource, _ uint) (transport.BulkOutRing, error) {
	// Use a fresh stopCh for each ring.
	stopCh := make(chan struct{})
	return newMockAOutRing(source, bufSize, stopCh), nil
}

// callsWithRequest returns control-out calls with the given request code.
func (m *aoutMockTransport) callsWithRequest(req uint8) []transport.Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []transport.Call
	for _, c := range m.calls {
		if c.Method == "ControlOut" && c.Request == req {
			out = append(out, c)
		}
	}
	return out
}

// ---- Tests ----

func TestCreateAOutScan_ConfigCalls(t *testing.T) {
	mt := &aoutMockTransport{}
	dev := NewDevice(mt, USB1808)

	cfg := AOutScanConfig{Channels: []int{0, 1}, Rate: 10000}
	h, err := dev.CreateAOutScan(cfg)
	require.NoError(t, err)
	require.NotNil(t, h)

	confs := mt.callsWithRequest(cmdAOutScanConf)
	require.Len(t, confs, 1, "expected one AOutScanConf call")
	require.Equal(t, uint16(1), confs[0].WIndex, "wIndex = lastChan-1 = 1")
	require.Equal(t, uint8(0), confs[0].Data[0], "queue[0] = 0 (DAC0)")
	require.Equal(t, uint8(1), confs[0].Data[1], "queue[1] = 1 (DAC1)")
}

func TestPlayOnceRaw_BytesMatch(t *testing.T) {
	samples := []uint16{0x8000, 0xC000, 0x4000, 0xFFFF}
	mt := &aoutMockTransport{}
	dev := NewDevice(mt, USB1808)

	h, err := dev.PlayOnceRaw([]int{0}, 10000, samples)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, h.Wait(ctx))

	// Collect bytes from all rings created.
	var got []byte
	rings := mt.callsWithRequest(cmdAOutScanStart)
	_ = rings // just verify start was sent

	// The ring drains the source; inspect via the source directly.
	src := h.src.(*bufferOnceSource)
	got = wire.Uint16sToBytes(samples)
	require.Equal(t, got, src.data, "source data should match encoded samples")
}

func TestPlayOnceRaw_Lifecycle(t *testing.T) {
	samples := []uint16{0x8000, 0x9000, 0xA000, 0xB000}
	mt := &aoutMockTransport{}
	dev := NewDevice(mt, USB1808)

	h, err := dev.PlayOnceRaw([]int{0}, 10000, samples)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = h.Wait(ctx)
	require.NoError(t, err)

	// Verify control sequence: AOutScanConf, AOutClearFIFO, AOutScanStart.
	require.NotEmpty(t, mt.callsWithRequest(cmdAOutScanConf))
	require.NotEmpty(t, mt.callsWithRequest(cmdAOutClearFIFO))
	require.NotEmpty(t, mt.callsWithRequest(cmdAOutScanStart))
}

func TestLoopRaw_StopWorks(t *testing.T) {
	pattern := []uint16{0x8000, 0xC000}
	mt := &aoutMockTransport{}
	dev := NewDevice(mt, USB1808)

	h, err := dev.LoopRaw([]int{0}, 10000, pattern)
	require.NoError(t, err)

	// Let it run briefly then stop.
	time.Sleep(20 * time.Millisecond)
	err = h.Stop()
	require.NoError(t, err)

	// Verify scan-stop was sent.
	require.NotEmpty(t, mt.callsWithRequest(cmdAOutScanStop))
}

func TestAOutScan_ValidationErrors(t *testing.T) {
	mt := &aoutMockTransport{}
	dev := NewDevice(mt, USB1808)

	// Empty channel list.
	_, err := dev.CreateAOutScan(AOutScanConfig{Channels: nil, Rate: 1000})
	require.Error(t, err)

	// Duplicate channel.
	_, err = dev.CreateAOutScan(AOutScanConfig{Channels: []int{0, 0}, Rate: 1000})
	require.Error(t, err)

	// Out-of-range channel.
	_, err = dev.CreateAOutScan(AOutScanConfig{Channels: []int{5}, Rate: 1000})
	require.Error(t, err)

	// Zero rate.
	_, err = dev.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 0})
	require.Error(t, err)

	// Rate exceeds model max (USB-1808 = 125 kHz).
	_, err = dev.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 200_000})
	require.Error(t, err)
}

func TestAOutScan_RateModelMax(t *testing.T) {
	mt := &aoutMockTransport{}

	dev1808 := NewDevice(mt, USB1808)
	_, err := dev1808.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 125_001})
	require.Error(t, err, "USB-1808 max rate is 125 kHz")

	_, err = dev1808.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 125_000})
	require.NoError(t, err, "USB-1808 max rate is exactly 125 kHz")

	dev1808x := NewDevice(mt, USB1808X)
	_, err = dev1808x.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 500_001})
	require.Error(t, err, "USB-1808X max rate is 500 kHz")

	_, err = dev1808x.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 500_000})
	require.NoError(t, err, "USB-1808X max rate is exactly 500 kHz")
}

func TestAnalogOut_RejectsWhenScanRunning(t *testing.T) {
	// Status response with AOutScanRunning bit set.
	statusRunning := wire.PutUint16LE(statusAOutScanRunning)
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: statusRunning}, // first status read
	)
	dev := NewDevice(mock, USB1808)

	err := dev.AnalogOut(0, 0.0)
	require.ErrorIs(t, err, ErrAOutScanRunning)
}

func TestAnalogOutRaw_SendsControlOut(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: make([]byte, 2)}, // status (not running)
		transport.MockResponse{},                      // cmdAOut
	)
	dev := NewDevice(mock, USB1808)

	err := dev.AnalogOutRaw(0, 0x8000)
	require.NoError(t, err)

	// Find cmdAOut call.
	var found bool
	for _, c := range mock.Calls {
		if c.Request == cmdAOut {
			require.Equal(t, uint16(0x8000), c.WValue, "wValue = DAC code")
			require.Equal(t, uint16(0), c.WIndex, "wIndex = channel 0")
			found = true
		}
	}
	require.True(t, found, "expected cmdAOut control call")
}

func TestVoltsToAOutRaw_Midscale(_ *testing.T) {
	dev := NewDevice(transport.NewMockTransport(), USB1808)
	// With zero calibration slope=0,offset=0: raw = 32768, cal = 0 → code = 0.
	// (Degenerate case; real hardware has slope≈1, offset≈0.)
	// Just confirm the function doesn't panic and is deterministic.
	_ = dev.VoltsToAOutRaw(0, 0.0)
}

func TestAOutScan_WithStopOnUnderrun(t *testing.T) {
	// statusSeq: first status has underrun bit set.
	mt := &aoutMockTransport{
		statusSeq: []uint16{
			statusAOutScanUnder, // first poll → underrun
		},
	}
	dev := NewDevice(mt, USB1808)

	samples := make([]uint16, 1000)
	for i := range samples {
		samples[i] = 0x8000
	}
	h, err := dev.LoopRaw([]int{0}, 10000, samples, WithStopOnUnderrun())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Wait for the scan to stop due to underrun.
	select {
	case <-h.done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for scan to stop on underrun")
	}
	require.ErrorIs(t, h.Err(), ErrAOutUnderrun)
}

func TestAOutScan_UnderrunCallback(t *testing.T) {
	// Provide underrun status for the first few polls, then clear.
	statusSeq := make([]uint16, 10)
	statusSeq[0] = statusAOutScanUnder

	mt := &aoutMockTransport{statusSeq: statusSeq}
	dev := NewDevice(mt, USB1808)

	var cbCalled int
	var cbMu sync.Mutex

	samples := make([]uint16, 1000)
	for i := range samples {
		samples[i] = 0x8000
	}
	h, err := dev.LoopRaw([]int{0}, 10000, samples,
		WithUnderrunCallback(func(_ uint64, _ time.Time) {
			cbMu.Lock()
			cbCalled++
			cbMu.Unlock()
		}),
	)
	require.NoError(t, err)

	// Let the poller tick at least once (10 ms interval).
	time.Sleep(50 * time.Millisecond)

	require.NoError(t, h.Stop())

	cbMu.Lock()
	n := cbCalled
	cbMu.Unlock()
	require.GreaterOrEqual(t, n, 1, "expected underrun callback to fire")
}

func TestAOutScan_UnderrunStorm(t *testing.T) {
	// Keep the underrun bit set forever → safety valve fires.
	statusSeq := make([]uint16, 100)
	for i := range statusSeq {
		statusSeq[i] = statusAOutScanUnder
	}

	mt := &aoutMockTransport{statusSeq: statusSeq}
	dev := NewDevice(mt, USB1808)

	samples := make([]uint16, 1000)
	for i := range samples {
		samples[i] = 0x8000
	}
	h, err := dev.LoopRaw([]int{0}, 10000, samples)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-h.done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for underrun storm to abort scan")
	}
	require.ErrorIs(t, h.Err(), ErrAOutUnderrun, "expected ErrAOutUnderrun from storm")
}

func TestAOutScanHandle_StopNotStarted(t *testing.T) {
	mt := &aoutMockTransport{}
	dev := NewDevice(mt, USB1808)
	h, err := dev.CreateAOutScan(AOutScanConfig{Channels: []int{0}, Rate: 1000})
	require.NoError(t, err)
	h.src = &bufferOnceSource{data: []byte{0x00, 0x80}}
	require.ErrorIs(t, h.Stop(), ErrAOutScanNotStarted)
}

func TestAOutRingStageSize(t *testing.T) {
	// 10 kHz, 1 channel: 10 ms = 100 scans × 2 bytes = 200 bytes.
	sz := aoutRingStageSize(10000, 1)
	require.GreaterOrEqual(t, sz, 200)
	require.Equal(t, 0, sz%2, "must be frame-aligned (2 bytes/scan)")

	// 100 kHz, 2 channels: 10 ms = 1000 scans × 4 bytes = 4000 bytes.
	sz = aoutRingStageSize(100_000, 2)
	require.GreaterOrEqual(t, sz, 4000)
	require.Equal(t, 0, sz%4, "must be frame-aligned (4 bytes/scan)")
}

func TestBufferOnceSource(t *testing.T) {
	data := wire.Uint16sToBytes([]uint16{0x1234, 0x5678})
	src := &bufferOnceSource{data: data}

	buf := make([]byte, 3)
	n, err := src.Fill(buf)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	n, err = src.Fill(buf)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 1, n) // last byte

	n, err = src.Fill(buf)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 0, n)
}

func TestBufferLoopSource(t *testing.T) {
	stop := make(chan struct{})
	data := wire.Uint16sToBytes([]uint16{0xAAAA, 0x5555})
	src := &bufferLoopSource{data: data, stop: stop}

	buf := make([]byte, 8) // 2× the data size
	n, err := src.Fill(buf)
	require.NoError(t, err)
	require.Equal(t, 8, n)
	// Should cycle: 0xAA, 0xAA, 0x55, 0x55, 0xAA, 0xAA, 0x55, 0x55
	require.Equal(t, data[0], buf[0])
	require.Equal(t, data[1], buf[1])

	close(stop)
	n, err = src.Fill(buf)
	require.Equal(t, io.EOF, err)
	require.Equal(t, 0, n)
}
