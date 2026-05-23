package device

import (
	"fmt"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

type scanState int

const (
	scanIdle scanState = iota
	scanRunning
	scanStopped
)

// Chunk is a batch of scan frames delivered by [ScanHandle.Chunks].
type Chunk struct {
	// Data contains raw frame data (packed little-endian uint32s).
	Data []byte
	// FrameStart is the index of the first frame in this chunk.
	FrameStart int
	// FrameCount is the number of complete frames in Data.
	FrameCount int
}

// ScanHandle represents a configured scan that can be started and stopped.
// Data is delivered via the Chunks channel as [Chunk] values.
type ScanHandle struct {
	dev      *Device
	channels []ChannelConfig
	rate     int
	count    uint32
	options  uint8
	opts     scanOptions

	// frameSize is the number of bytes per frame (nChannels * 4).
	frameSize int
	chunks    chan Chunk
	stop      chan struct{}
	done      chan struct{}
	err       error
	mu        sync.Mutex
	state     scanState
	stopOnce  sync.Once
}

// CreateScan configures the hardware for scanning and returns a handle.
// The scan is not started until Start is called.
func (d *Device) CreateScan(cfg ScanConfig, opts ...ScanOption) (*ScanHandle, error) {
	if err := validateScanConfig(cfg); err != nil {
		return nil, err
	}

	o := scanOptions{
		pipelineDepth: DefaultPipelineDepth,
	}
	for _, opt := range opts {
		opt(&o)
	}

	// Configure ADC for analog channels.
	adcBuf := make([]byte, NumAInChannels)
	for _, ch := range cfg.Channels {
		if ch.Type == ChannelTypeAnalog && ch.Index < NumAInChannels {
			adcBuf[ch.Index] = uint8(ch.Range) | (uint8(ch.Mode) << 2)
		}
	}
	d.mu.Lock()
	err := d.transport.ControlOut(cmdADCSetup, 0, 0, adcBuf)
	d.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("ADC setup: %w", err)
	}

	// Configure scan queue.
	queueBuf := make([]byte, MaxAInQueue)
	for i, ch := range cfg.Channels {
		queueBuf[i] = uint8(ch.Index)
	}
	lastChan := uint16(len(cfg.Channels))
	d.mu.Lock()
	err = d.transport.ControlOut(cmdAInConfig, 0, lastChan-1, queueBuf)
	d.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("scan queue config: %w", err)
	}

	nCh := len(cfg.Channels)
	h := &ScanHandle{
		dev:       d,
		channels:  append([]ChannelConfig(nil), cfg.Channels...),
		rate:      cfg.Rate,
		count:     cfg.Count,
		options:   cfg.Options,
		opts:      o,
		frameSize: nCh * 4,
		chunks:    make(chan Chunk, o.pipelineDepth),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	return h, nil
}

// Chunks returns the channel delivering scan data.
// Each [Chunk] contains one or more complete frames along with the
// frame offset, which allows consumers to detect gaps from dropped chunks.
func (h *ScanHandle) Chunks() <-chan Chunk {
	return h.chunks
}

// Start begins the scan, launching reader goroutines.
func (h *ScanHandle) Start() error {
	h.mu.Lock()
	if h.state != scanIdle {
		h.mu.Unlock()
		return ErrScanRunning
	}
	h.state = scanRunning
	h.mu.Unlock()

	// Clear FIFO.
	h.dev.mu.Lock()
	err := h.dev.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
	h.dev.mu.Unlock()
	if err != nil {
		h.mu.Lock()
		h.state = scanIdle
		h.mu.Unlock()
		return fmt.Errorf("clear FIFO: %w", err)
	}

	ar, ok := h.dev.transport.(transport.AsyncBulkReader)
	if !ok {
		close(h.done)
		return fmt.Errorf("transport does not support async bulk reads")
	}
	return h.startAsync(ar)
}

// Stop stops the scan and waits for all readers to finish.
func (h *ScanHandle) Stop() error {
	h.mu.Lock()
	switch h.state {
	case scanIdle:
		h.mu.Unlock()
		return ErrScanNotStarted
	case scanStopped:
		h.mu.Unlock()
		return ErrScanStopped
	}
	h.state = scanStopped
	h.mu.Unlock()

	h.stopOnce.Do(func() { close(h.stop) })

	// Send stop command.
	h.dev.mu.Lock()
	_ = h.dev.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
	_ = h.dev.transport.ControlOut(cmdAInBulkFlush, 0, 0, nil)
	h.dev.mu.Unlock()

	<-h.done
	return h.Err()
}

// Err returns any error that occurred during the scan.
func (h *ScanHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// Channels returns the configured channel list.
func (h *ScanHandle) Channels() []ChannelConfig {
	return h.channels
}

// Rate returns the configured sample rate.
func (h *ScanHandle) Rate() int {
	return h.rate
}

// FrameSize returns the number of bytes per frame.
func (h *ScanHandle) FrameSize() int {
	return h.frameSize
}

func (h *ScanHandle) setErr(err error) {
	h.mu.Lock()
	if h.err == nil {
		h.err = err
	}
	h.mu.Unlock()
}

// startAsync launches the scan using an async bulk transfer ring.
func (h *ScanHandle) startAsync(ar transport.AsyncBulkReader) error {
	nCh := len(h.channels)
	rate := float64(h.rate)
	bufSize := ringStageSize(rate, nCh)

	// Compute packet size.
	packetSize := uint8(0xFF)
	samplesPerScan := nCh
	if rate*float64(samplesPerScan) < MaxPacketSize/4 {
		packetSize = uint8(min(samplesPerScan, 256) - 1)
	}

	ring, err := ar.NewBulkRing(epBulkIn, bufSize, ringTransferCount, h.opts.pipelineDepth, 0)
	if err != nil {
		close(h.done)
		return err
	}

	// Start the scan.
	pacerPeriod := wire.PacerPeriod(rate)
	payload := wire.AInScanStartPayload(h.count, 0, pacerPeriod, packetSize, h.options)
	h.dev.mu.Lock()
	err = h.dev.transport.ControlOut(cmdAInScanStart, 0, 0, payload)
	h.dev.mu.Unlock()
	if err != nil {
		ring.Stop()
		close(h.done)
		return err
	}

	go h.runAsyncReader(ring)
	return nil
}

func (h *ScanHandle) runAsyncReader(ring transport.BulkRing) {
	defer close(h.done)
	defer close(h.chunks)
	defer ring.Stop()
	defer func() {
		h.stopOnce.Do(func() { close(h.stop) })
	}()

	total := h.count
	var framesRead uint32

	// Monitor for stop signal.
	go func() {
		<-h.stop
		h.dev.mu.Lock()
		_ = h.dev.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
		h.dev.mu.Unlock()
		ring.Stop()
	}()

	// Watchdog for finite scans (same rationale as runSyncReaders).
	if h.count > 0 {
		expected := time.Duration(float64(h.count) / float64(h.rate) * float64(time.Second))
		watchdog := time.AfterFunc(expected+5*time.Second, func() {
			h.stopOnce.Do(func() { close(h.stop) })
		})
		defer watchdog.Stop()
	}

	for result := range ring.Results() {
		if result.Err != nil {
			h.dev.mu.Lock()
			sd, se := h.dev.transport.ControlIn(cmdStatus, 0, 0, 2)
			if se == nil && Status(wire.Uint16LE(sd)).AInScanOverrun() {
				_ = h.dev.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
				_ = h.dev.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
				h.setErr(ErrScanOverrun)
			} else {
				h.setErr(result.Err)
			}
			h.dev.mu.Unlock()
			return
		}

		data := result.Data
		data = data[:len(data)/h.frameSize*h.frameSize]

		if total > 0 {
			remaining := int(total-framesRead) * h.frameSize
			if len(data) > remaining {
				data = data[:remaining]
			}
		}

		nFrames := len(data) / h.frameSize
		if nFrames > 0 {
			select {
			case h.chunks <- Chunk{
				Data:       data,
				FrameStart: int(framesRead),
				FrameCount: nFrames,
			}:
			case <-h.stop:
				return
			}
		}

		framesRead += uint32(nFrames)
		if total > 0 && framesRead >= total {
			return
		}
	}
}

// ringStageSize computes the buffer size in bytes for each async ring transfer.
// Targets ~10ms of data, aligned to MaxPacketSize, capped at ringMaxStageSize.
func ringStageSize(rate float64, nChannels int) int {
	// 10ms of data (in scans)
	n := max(int(rate*0.01), 1)

	bytesPerScan := nChannels * 4
	totalBytes := min(n*bytesPerScan, ringMaxStageSize)

	if totalBytes >= MaxPacketSize {
		totalBytes = (totalBytes / MaxPacketSize) * MaxPacketSize
	}

	if totalBytes < MaxPacketSize {
		totalBytes = MaxPacketSize
	}

	totalBytes = max((totalBytes/bytesPerScan)*bytesPerScan, bytesPerScan)
	return totalBytes
}
