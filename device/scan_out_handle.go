package device

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

// samplesSource fills the next staging buffer with sample bytes for the
// bulk-OUT ring. Identical contract to transport.BulkOutSource.
type samplesSource interface {
	Fill(buf []byte) (n int, err error)
}

// AOutScanHandle represents a configured analog output scan that can be
// started and stopped. Parallel to ScanHandle for analog input.
type AOutScanHandle struct {
	dev  *Device
	cfg  AOutScanConfig
	opts aoutScanOptions

	// src is the sample source; set by CreateAOutScan helpers before Start.
	src samplesSource

	frameSize int // bytes per output scan (nChannels * 2)
	bufSize   int // bytes per URB staging buffer

	stop    chan struct{}
	done    chan struct{}
	underrun chan struct{} // underrun events from the status poller

	state    scanState
	stopOnce sync.Once
	mu       sync.Mutex
	err      error

	// payload saved for scan restart after underrun.
	payload []byte
}

// CreateAOutScan configures the hardware scan queue and returns a handle.
// The scan is not started until Start is called. Callers must set h.src
// before calling Start (use PlayOnce, PlayOnceRaw, Loop, or LoopRaw for
// convenience wrappers that do this automatically).
func (d *Device) CreateAOutScan(cfg AOutScanConfig, opts ...AOutScanOption) (*AOutScanHandle, error) {
	if err := validateAOutScanConfig(cfg, d.model); err != nil {
		return nil, err
	}

	o := aoutScanOptions{pipelineDepth: DefaultAOutPipelineDepth}
	for _, opt := range opts {
		opt(&o)
	}

	// Configure AOut scan queue (cmdAOutScanConf).
	// Payload: MaxAOutQueue bytes; only the first len(cfg.Channels) are used.
	queueBuf := make([]byte, MaxAOutQueue)
	for i, ch := range cfg.Channels {
		queueBuf[i] = uint8(ch)
	}
	d.mu.Lock()
	err := d.transport.ControlOut(cmdAOutScanConf, 0, uint16(len(cfg.Channels)-1), queueBuf)
	d.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("AOut scan config: %w", err)
	}

	nCh := len(cfg.Channels)
	h := &AOutScanHandle{
		dev:       d,
		cfg:       cfg,
		opts:      o,
		frameSize: nCh * 2, // uint16 per channel
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		underrun:  make(chan struct{}, 1),
	}
	return h, nil
}

// Start begins the analog output scan. src must be set before calling Start.
func (h *AOutScanHandle) Start() error {
	h.mu.Lock()
	if h.state != scanIdle {
		h.mu.Unlock()
		if h.state == scanRunning {
			return ErrAOutScanRunning
		}
		return ErrAOutScanStopped
	}
	h.state = scanRunning
	h.mu.Unlock()

	if h.src == nil {
		h.mu.Lock()
		h.state = scanIdle
		h.mu.Unlock()
		return fmt.Errorf("usb1808: AOut scan source not set")
	}

	// Clear the AOut FIFO.
	h.dev.mu.Lock()
	err := h.dev.transport.ControlOut(cmdAOutClearFIFO, 0, 0, nil)
	h.dev.mu.Unlock()
	if err != nil {
		h.mu.Lock()
		h.state = scanIdle
		h.mu.Unlock()
		return fmt.Errorf("AOut clear FIFO: %w", err)
	}

	aw, ok := h.dev.transport.(transport.AsyncBulkWriter)
	if !ok {
		close(h.done)
		return fmt.Errorf("usb1808: transport does not support async bulk writes")
	}

	h.bufSize = aoutRingStageSize(float64(h.cfg.Rate), len(h.cfg.Channels))

	ring, err := aw.NewBulkOutRing(epBulkOut, h.bufSize, aoutRingTransferCount, h.src, 0)
	if err != nil {
		close(h.done)
		return fmt.Errorf("AOut bulk ring: %w", err)
	}

	// Build and send the scan start payload.
	pacerPeriod := wire.PacerPeriod(float64(h.cfg.Rate))
	payload := wire.AOutScanStartPayload(h.cfg.Count, 0, pacerPeriod, h.cfg.Options)
	h.payload = payload

	h.dev.mu.Lock()
	err = h.dev.transport.ControlOut(cmdAOutScanStart, 0, 0, payload)
	h.dev.mu.Unlock()
	if err != nil {
		ring.Stop()
		close(h.done)
		return fmt.Errorf("AOut scan start: %w", err)
	}

	go h.runAOutLoop(ring, aw)
	return nil
}

// Stop aborts the scan and waits for the goroutine to exit.
func (h *AOutScanHandle) Stop() error {
	h.mu.Lock()
	switch h.state {
	case scanIdle:
		h.mu.Unlock()
		return ErrAOutScanNotStarted
	case scanStopped:
		h.mu.Unlock()
		return ErrAOutScanStopped
	}
	h.state = scanStopped
	h.mu.Unlock()

	h.stopOnce.Do(func() { close(h.stop) })

	// Send stop commands directly (not relying solely on the goroutine)
	// so callers can depend on the hardware being stopped after Stop returns.
	h.dev.mu.Lock()
	_ = h.dev.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
	_ = h.dev.transport.ControlOut(cmdAOutClearFIFO, 0, 0, nil)
	h.dev.mu.Unlock()

	<-h.done
	return h.Err()
}

// Wait blocks until the scan finishes naturally (Count > 0) or ctx is done.
// For continuous scans (Count == 0), Wait blocks until Stop is called or
// ctx is cancelled; it will never return nil on its own.
func (h *AOutScanHandle) Wait(ctx context.Context) error {
	select {
	case <-h.done:
		return h.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Err returns any error from the scan.
func (h *AOutScanHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// Channels returns the configured DAC channel queue.
func (h *AOutScanHandle) Channels() []int { return h.cfg.Channels }

// Rate returns the configured output rate in Hz per channel.
func (h *AOutScanHandle) Rate() int { return h.cfg.Rate }

func (h *AOutScanHandle) setErr(err error) {
	h.mu.Lock()
	if h.err == nil {
		h.err = err
	}
	h.mu.Unlock()
}

// runAOutLoop is the main goroutine for an AOut scan. It monitors for
// natural completion, stop requests, and underruns.
func (h *AOutScanHandle) runAOutLoop(ring transport.BulkOutRing, aw transport.AsyncBulkWriter) {
	var wg sync.WaitGroup

	// Close h.done only after all inner goroutines have exited so that
	// Stop() cannot return while any goroutine still holds h.dev.mu.
	defer func() {
		h.stopOnce.Do(func() { close(h.stop) })
		wg.Wait()
		close(h.done)
	}()

	// Stop monitor: sends hardware stop command when stop is requested.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-h.stop
		h.dev.mu.Lock()
		_ = h.dev.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
		_ = h.dev.transport.ControlOut(cmdAOutClearFIFO, 0, 0, nil)
		h.dev.mu.Unlock()
		ring.Stop()
	}()

	// Status poller: signals underrun channel when AOutScanUnder bit is set.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				h.dev.mu.Lock()
				st, err := h.dev.status()
				h.dev.mu.Unlock()
				if err != nil {
					continue
				}
				if st.AOutScanUnderrun() {
					select {
					case h.underrun <- struct{}{}:
					default:
					}
				}
			}
		}
	}()

	// Watchdog for finite scans.
	if h.cfg.Count > 0 {
		expected := time.Duration(float64(h.cfg.Count) / float64(h.cfg.Rate) * float64(time.Second))
		watchdog := time.AfterFunc(expected+5*time.Second, func() {
			h.stopOnce.Do(func() { close(h.stop) })
		})
		defer watchdog.Stop()
	}

	var recentUnderruns int
	var firstUnderrunTime time.Time

	for {
		select {
		case <-ring.Done():
			if err := ring.Err(); err != nil {
				h.setErr(err)
			}
			ring.Stop()
			return

		case <-h.stop:
			ring.Stop()
			return

		case <-h.underrun:
			now := time.Now()

			if h.opts.stopOnUnderrun {
				h.setErr(ErrAOutUnderrun)
				return
			}

			// Safety valve: too many underruns in rapid succession.
			if recentUnderruns > 0 && now.Sub(firstUnderrunTime) < time.Second {
				recentUnderruns++
				if recentUnderruns >= maxAOutUnderrunsPerSecond {
					h.setErr(fmt.Errorf("%w: %d underruns in %v",
						ErrAOutUnderrun, recentUnderruns, now.Sub(firstUnderrunTime)))
					return
				}
			} else {
				recentUnderruns = 1
				firstUnderrunTime = now
			}

			if h.opts.onUnderrun != nil {
				bytesSent := ring.Stats().BytesSent
				frames := bytesSent / uint64(h.frameSize)
				h.opts.onUnderrun(frames, now)
			}

			// Restart the scan after underrun.
			newRing, err := h.restartAOutScan(aw)
			if err != nil {
				h.setErr(fmt.Errorf("restart after underrun: %w", err))
				return
			}
			ring.Stop()
			ring = newRing
		}
	}
}

// restartAOutScan stops the current ring, resets the source, creates a new
// ring, and restarts the hardware scan.
func (h *AOutScanHandle) restartAOutScan(aw transport.AsyncBulkWriter) (transport.BulkOutRing, error) {
	// Reset the source position if it supports it.
	if rs, ok := h.src.(interface{ Reset() }); ok {
		rs.Reset()
	}

	h.dev.mu.Lock()
	_ = h.dev.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
	_ = h.dev.transport.ControlOut(cmdAOutClearFIFO, 0, 0, nil)
	h.dev.mu.Unlock()

	ring, err := aw.NewBulkOutRing(epBulkOut, h.bufSize, aoutRingTransferCount, h.src, 0)
	if err != nil {
		return nil, err
	}

	h.dev.mu.Lock()
	err = h.dev.transport.ControlOut(cmdAOutScanStart, 0, 0, h.payload)
	h.dev.mu.Unlock()
	if err != nil {
		ring.Stop()
		return nil, err
	}
	return ring, nil
}

// aoutRingStageSize computes the bulk-OUT staging buffer size in bytes.
// Targets ~10 ms of data, frame-aligned, capped at aoutRingMaxStageSize.
// AOut samples are 2 bytes each (uint16), compared to 4 bytes for AIn.
// No 512-byte alignment is applied: USB bulk OUT transfers do not need to
// be multiples of wMaxPacketSize on the host side (ZLP is handled separately).
func aoutRingStageSize(rate float64, nChannels int) int {
	n := max(int(rate*0.01), 1) // scans in 10 ms
	bytesPerScan := nChannels * 2
	totalBytes := min(n*bytesPerScan, aoutRingMaxStageSize)
	// Ensure at least one frame.
	totalBytes = max((totalBytes/bytesPerScan)*bytesPerScan, bytesPerScan)
	return totalBytes
}

// bufferOnceSource plays a fixed byte slice once then signals EOF.
type bufferOnceSource struct {
	data   []byte
	offset int
}

func (s *bufferOnceSource) Fill(buf []byte) (int, error) {
	if s.offset >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(buf, s.data[s.offset:])
	s.offset += n
	if s.offset >= len(s.data) {
		return n, io.EOF
	}
	return n, nil
}

func (s *bufferOnceSource) Reset() { s.offset = 0 }

// bufferLoopSource cycles through a fixed byte slice until stop is closed.
type bufferLoopSource struct {
	data   []byte
	offset int
	stop   <-chan struct{}
}

func (s *bufferLoopSource) Fill(buf []byte) (int, error) {
	select {
	case <-s.stop:
		return 0, io.EOF
	default:
	}
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := 0
	for n < len(buf) {
		copied := copy(buf[n:], s.data[s.offset:])
		s.offset = (s.offset + copied) % len(s.data)
		n += copied
	}
	return n, nil
}

func (s *bufferLoopSource) Reset() { s.offset = 0 }

// PlayOnce outputs a single-shot waveform in volts and waits for completion.
// samples are interleaved by the channel queue order.
func (d *Device) PlayOnce(channels []int, rate int, samples []float64, opts ...AOutScanOption) (*AOutScanHandle, error) {
	raw := make([]uint16, len(samples))
	for i, v := range samples {
		raw[i] = d.VoltsToAOutRaw(channels[i%len(channels)], v)
	}
	return d.PlayOnceRaw(channels, rate, raw, opts...)
}

// PlayOnceRaw outputs a single-shot waveform in raw DAC codes and waits for
// completion. samples are interleaved by the channel queue order.
func (d *Device) PlayOnceRaw(channels []int, rate int, samples []uint16, opts ...AOutScanOption) (*AOutScanHandle, error) {
	cfg := AOutScanConfig{
		Channels: channels,
		Rate:     rate,
		Count:    uint32(len(samples) / len(channels)),
	}
	h, err := d.CreateAOutScan(cfg, opts...)
	if err != nil {
		return nil, err
	}
	h.src = &bufferOnceSource{data: wire.Uint16sToBytes(samples)}
	if err := h.Start(); err != nil {
		return nil, err
	}
	return h, nil
}

// Loop outputs a waveform in volts continuously until Stop is called.
// samples must contain exactly one period (interleaved by channel queue order).
func (d *Device) Loop(channels []int, rate int, samples []float64, opts ...AOutScanOption) (*AOutScanHandle, error) {
	raw := make([]uint16, len(samples))
	for i, v := range samples {
		raw[i] = d.VoltsToAOutRaw(channels[i%len(channels)], v)
	}
	return d.LoopRaw(channels, rate, raw, opts...)
}

// LoopRaw outputs a waveform in raw DAC codes continuously until Stop is called.
// samples must contain exactly one period (interleaved by channel queue order).
func (d *Device) LoopRaw(channels []int, rate int, samples []uint16, opts ...AOutScanOption) (*AOutScanHandle, error) {
	cfg := AOutScanConfig{
		Channels: channels,
		Rate:     rate,
		Count:    0, // continuous
	}
	h, err := d.CreateAOutScan(cfg, opts...)
	if err != nil {
		return nil, err
	}
	h.src = &bufferLoopSource{
		data: wire.Uint16sToBytes(samples),
		stop: h.stop,
	}
	if err := h.Start(); err != nil {
		return nil, err
	}
	return h, nil
}
