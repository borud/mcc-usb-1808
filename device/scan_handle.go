package device

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

// ScanHandle represents a configured scan that can be started and stopped.
// Data is delivered via the Chunks channel as raw byte slices.
type ScanHandle struct {
	dev      *Device
	channels []ChannelConfig
	rate     int
	count    uint32
	options  uint8
	opts     scanOptions

	frameSize int // bytes per frame (nChannels * 4)
	chunks    chan []byte
	stop      chan struct{}
	done      chan struct{}
	err       error
	mu        sync.Mutex
}

// CreateScan configures the hardware for scanning and returns a handle.
// The scan is not started until Start is called.
func (d *Device) CreateScan(cfg ScanConfig, opts ...ScanOption) (*ScanHandle, error) {
	if err := validateScanConfig(cfg); err != nil {
		return nil, err
	}

	o := scanOptions{
		pipelineDepth:     DefaultPipelineDepth,
		concurrentReaders: DefaultConcurrentReaders,
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
		chunks:    make(chan []byte, o.pipelineDepth),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	return h, nil
}

// Chunks returns the channel delivering raw scan data.
// Each slice contains one or more complete frames.
func (h *ScanHandle) Chunks() <-chan []byte {
	return h.chunks
}

// Start begins the scan, launching reader goroutines.
func (h *ScanHandle) Start() error {
	// Clear FIFO.
	h.dev.mu.Lock()
	_ = h.dev.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
	h.dev.mu.Unlock()

	// Check if async path is available.
	if ar, ok := h.dev.transport.(transport.AsyncBulkReader); ok {
		return h.startAsync(ar)
	}
	return h.startSync()
}

// Stop stops the scan and waits for all readers to finish.
func (h *ScanHandle) Stop() error {
	select {
	case <-h.stop:
		// Already stopped.
	default:
		close(h.stop)
	}

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

// startSync launches the scan using synchronous bulk reads.
func (h *ScanHandle) startSync() error {
	nCh := len(h.channels)
	rate := float64(h.rate)
	batch := scanBatchSize(rate, nCh)
	batchBytes := batch * nCh * 4
	readTimeout := scanReadTimeout(rate, batch)

	// Compute packet size.
	packetSize := uint8(0xFF)
	samplesPerScan := nCh
	if rate*float64(samplesPerScan) < MaxPacketSize/4 {
		packetSize = uint8(min(samplesPerScan, 256) - 1)
	}

	// Start the scan.
	pacerPeriod := wire.PacerPeriod(rate)
	payload := wire.AInScanStartPayload(h.count, 0, pacerPeriod, packetSize, h.options)
	h.dev.mu.Lock()
	err := h.dev.transport.ControlOut(cmdAInScanStart, 0, 0, payload)
	h.dev.mu.Unlock()
	if err != nil {
		close(h.done)
		return err
	}

	go h.runSyncReaders(batchBytes, readTimeout)
	return nil
}

func (h *ScanHandle) runSyncReaders(batchBytes int, readTimeout time.Duration) {
	defer close(h.done)
	defer close(h.chunks)

	numReaders := h.opts.concurrentReaders
	poolSize := h.opts.pipelineDepth + numReaders
	free := make(chan []byte, poolSize)
	for range poolSize {
		free <- make([]byte, batchBytes)
	}

	type readResult struct {
		buf []byte
		n   int
		err error
	}
	ch := make(chan readResult, h.opts.pipelineDepth)

	var readerWg sync.WaitGroup
	for range numReaders {
		readerWg.Add(1)
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer readerWg.Done()

			for {
				select {
				case <-h.stop:
					return
				default:
				}

				var buf []byte
				select {
				case buf = <-free:
				case <-h.stop:
					return
				}

				n, err := h.dev.transport.BulkReadInto(epBulkIn, buf, readTimeout)
				if err != nil && n == 0 {
					free <- buf
					// Check for overrun.
					h.dev.mu.Lock()
					sd, se := h.dev.transport.ControlIn(cmdStatus, 0, 0, 2)
					if se == nil {
						if Status(wire.Uint16LE(sd)).AInScanOverrun() {
							_ = h.dev.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
							_ = h.dev.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
							err = ErrScanOverrun
						}
					}
					h.dev.mu.Unlock()
					select {
					case ch <- readResult{nil, 0, err}:
					case <-h.stop:
					}
					return
				}

				select {
				case ch <- readResult{buf, n, nil}:
				case <-h.stop:
					free <- buf
					return
				}
			}
		}()
	}
	go func() {
		readerWg.Wait()
		close(ch)
	}()

	total := int(h.count)
	framesRead := 0

	for result := range ch {
		if result.err != nil {
			h.setErr(result.err)
			return
		}

		data := result.buf[:result.n]
		// Truncate to frame boundary.
		data = data[:len(data)/h.frameSize*h.frameSize]

		if total > 0 {
			remaining := (total - framesRead) * h.frameSize
			if len(data) > remaining {
				data = data[:remaining]
			}
		}

		if len(data) > 0 {
			select {
			case h.chunks <- data:
			case <-h.stop:
				free <- result.buf
				return
			}
		} else {
			free <- result.buf
		}

		framesRead += len(data) / h.frameSize
		if total > 0 && framesRead >= total {
			return
		}
	}
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

func (h *ScanHandle) runAsyncReader(ring *transport.BulkRing) {
	defer close(h.done)
	defer close(h.chunks)
	defer ring.Stop()

	total := int(h.count)
	framesRead := 0

	// Monitor for stop signal.
	go func() {
		<-h.stop
		h.dev.mu.Lock()
		_ = h.dev.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
		h.dev.mu.Unlock()
		ring.Stop()
	}()

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
			remaining := (total - framesRead) * h.frameSize
			if len(data) > remaining {
				data = data[:remaining]
			}
		}

		if len(data) > 0 {
			select {
			case h.chunks <- data:
			case <-h.stop:
				return
			}
		}

		framesRead += len(data) / h.frameSize
		if total > 0 && framesRead >= total {
			return
		}
	}
}
