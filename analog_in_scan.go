package usb1808

import (
	"context"
	"fmt"
	"iter"
	"runtime"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808/internal/wire"
)

// AnalogInScanConfig holds configuration for an analog input scan.
type AnalogInScanConfig struct {
	Channels      []int   // Scan queue channel selectors (0-12).
	Rate          float64 // Sample rate in Hz per channel.
	Count         uint32  // Total number of scans (0 = continuous).
	RetrigCount   uint32  // Scans per retrigger (0 = no retrigger).
	Options       uint8   // Scan option flags.
	PacketSize    uint8   // Samples-1 per USB packet (0xFF = max).
	PipelineDepth int     // Buffered read-ahead batches (0 = default 16).
}

// ConfigureAnalogInScan writes the input scan queue configuration.
func (d *Device) ConfigureAnalogInScan(channels []int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(channels) == 0 || len(channels) > MaxAInQueue {
		return fmt.Errorf("%w: queue length %d (max %d)", ErrInvalidChannel, len(channels), MaxAInQueue)
	}

	buf := make([]byte, MaxAInQueue)
	for i, ch := range channels {
		if ch < 0 || ch > 12 {
			return fmt.Errorf("%w: scan queue value %d", ErrInvalidChannel, ch)
		}
		buf[i] = uint8(ch)
	}
	lastChan := uint16(len(channels))
	if err := d.transport.ControlOut(cmdAInConfig, 0, lastChan-1, buf); err != nil {
		return err
	}
	d.ainScanQueue = append([]int(nil), channels...)
	return nil
}

// AnalogInScanConfig reads the current input scan queue configuration.
func (d *Device) AnalogInScanConfig(numChannels int) ([]uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdAInConfig, 0, uint16(numChannels-1), MaxAInQueue)
	if err != nil {
		return nil, err
	}
	return data[:numChannels], nil
}

// StartAnalogInScan starts an analog input scan with the given configuration.
func (d *Device) StartAnalogInScan(cfg AnalogInScanConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	packetSize := cfg.PacketSize
	if packetSize == 0 {
		packetSize = 0xFF
	}

	pacerPeriod := wire.PacerPeriod(cfg.Rate)
	payload := wire.AInScanStartPayload(cfg.Count, cfg.RetrigCount, pacerPeriod, packetSize, cfg.Options)

	return d.transport.ControlOut(cmdAInScanStart, 0, 0, payload)
}

// ReadAnalogInScanRaw reads analog input scan data from the bulk endpoint.
// It requests nChannels * nScans * 4 bytes in a single bulk transfer.
// If the transfer times out but some data was received, the partial data
// is returned without error. Only a timeout with zero bytes is an error.
//
// The returned slice may contain fewer samples than requested.
func (d *Device) ReadAnalogInScanRaw(_ context.Context, nChannels, nScans int, timeout time.Duration) ([]uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	totalBytes := nChannels * nScans * 4
	if totalBytes > maxTransferBytes {
		totalBytes = maxTransferBytes
	}

	data, err := d.transport.BulkRead(epBulkIn, totalBytes, timeout)
	if err != nil && len(data) == 0 {
		return nil, err
	}

	// Check for overrun.
	statusData, statusErr := d.transport.ControlIn(cmdStatus, 0, 0, 2)
	if statusErr == nil {
		st := Status(wire.Uint16LE(statusData))
		if st.AInScanOverrun() {
			_ = d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
			_ = d.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
			return nil, ErrScanOverrun
		}

		// Handle ZLP if transfer was 512-aligned and scan stopped.
		if len(data)%MaxPacketSize == 0 && !st.AInScanRunning() {
			_, _ = d.transport.BulkRead(epBulkIn, 2, 100*time.Millisecond)
		}
	}

	// Truncate to whole-sample boundary.
	nSamples := len(data) / 4
	result := make([]uint32, nSamples)
	for i := range nSamples {
		result[i] = wire.Uint32LE(data[i*4 : i*4+4])
	}
	return result, nil
}

// StopAnalogInScan stops a running analog input scan.
func (d *Device) StopAnalogInScan() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) analogInClearFIFO() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) analogInBulkFlush(count uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdAInBulkFlush, count, 0, nil)
}

// ReadAnalogInScan reads scan data and returns calibrated voltages.
// The scan queue must be configured first with ConfigureAnalogInScan.
// Returns a flat slice organized as [scan0_ch0, scan0_ch1, ..., scan1_ch0, ...].
func (d *Device) ReadAnalogInScan(ctx context.Context, nScans int) ([]float64, error) {
	channels := d.ainScanQueue
	nCh := len(channels)
	if nCh == 0 {
		return nil, fmt.Errorf("usb1808: scan queue not configured")
	}

	raw, err := d.ReadAnalogInScanRaw(ctx, nCh, nScans, usbTimeout)
	if err != nil {
		return nil, err
	}

	volts := make([]float64, len(raw))
	for i, v := range raw {
		ch := channels[i%nCh]
		if ch < NumAInChannels {
			volts[i] = d.AnalogInToVolts(v, ch, d.ainRanges[ch])
		} else {
			volts[i] = float64(v)
		}
	}
	return volts, nil
}

// maxTransferBytes is the maximum bytes per USB bulk read. Large single
// transfers can time out on macOS/libusb; 64 KiB is safe across platforms.
const maxTransferBytes = 64 * 1024

// scanBatchSize computes the number of scans to read per USB bulk transfer.
// Targets ~100ms worth of data per read to keep ahead of the device FIFO,
// capped at maxTransferBytes. The result is aligned to MaxPacketSize (512)
// boundaries to avoid USB bulk overflow errors.
//
// At low sample rates the batch is clamped so that the expected fill time
// stays well within the USB read timeout.
func scanBatchSize(rate float64, nChannels int) int {
	n := int(rate * 0.1) // 100ms of data
	if n < 1 {
		n = 1
	}
	bytesPerScan := nChannels * 4
	totalBytes := n * bytesPerScan
	if totalBytes > maxTransferBytes {
		totalBytes = maxTransferBytes
	}
	// At high rates, align to MaxPacketSize boundary for efficient bulk
	// transfers. At low rates (< 1 packet worth), just request what we
	// need — the device will send short packets.
	if totalBytes >= MaxPacketSize {
		totalBytes = (totalBytes / MaxPacketSize) * MaxPacketSize
	}
	return totalBytes / bytesPerScan
}

// scanReadTimeout computes the USB bulk read timeout for a given batch.
// It uses the expected fill time (nScans / rate) plus a fixed headroom
// to avoid spurious timeouts at low sample rates. The minimum is
// usbTimeout (2 s) so high-rate reads are unaffected.
func scanReadTimeout(rate float64, nScans int) time.Duration {
	fillTime := time.Duration(float64(nScans) / rate * float64(time.Second))
	timeout := fillTime + 2*time.Second
	if timeout < usbTimeout {
		timeout = usbTimeout
	}
	return timeout
}

// ScanAnalogIn returns a pull-based iterator that reads analog input scan data
// as calibrated voltages. Each iteration yields one scan frame (one value per
// channel in cfg.Channels). The iterator configures the scan queue, starts the
// scan, and stops it when iteration ends (via break, return, or error).
//
// The yielded slice is reused across iterations; callers must copy the data
// if they need to retain it beyond the current iteration.
//
// Channel ranges must be configured first with ConfigureAnalogIn.
func (d *Device) ScanAnalogIn(ctx context.Context, cfg AnalogInScanConfig) iter.Seq2[[]float64, error] {
	return func(yield func([]float64, error) bool) {
		nCh := len(cfg.Channels)
		frame := make([]float64, nCh)
		for raw, err := range d.ScanAnalogInRaw(ctx, cfg) {
			if err != nil {
				yield(nil, err)
				return
			}
			for i, ch := range cfg.Channels {
				v := raw[i]
				if ch < NumAInChannels {
					frame[i] = d.AnalogInToVolts(v, ch, d.ainRanges[ch])
				} else {
					frame[i] = float64(v)
				}
			}
			if !yield(frame, nil) {
				return
			}
		}
	}
}

// DefaultPipelineDepth is the number of bulk read results to buffer ahead
// of the consumer. At high sample rates each result represents ~10-55 ms of
// data, so 32 buffers provides 320 ms – 1.7 s of slack to absorb
// processing/IO jitter across the full rate range.
const DefaultPipelineDepth = 32

// DefaultConcurrentReaders is the number of goroutines concurrently issuing
// synchronous USB bulk reads. Each goroutine keeps one libusb_bulk_transfer
// in flight, so N goroutines means N transfers queued at the kernel level.
// This eliminates dead time between transfers that would cause device FIFO
// overflow at high sample rates.
const DefaultConcurrentReaders = 4

// ScanAnalogInRaw is like [Device.ScanAnalogIn] but yields raw uint32 values
// without voltage conversion. This is useful for capturing raw ADC codes.
//
// The yielded slice is reused across iterations; callers must copy the data
// if they need to retain it beyond the current iteration.
//
// Internally a background goroutine drains the USB bulk endpoint into a
// buffered pipeline so that host-side processing latency (disk writes, etc.)
// does not cause the device FIFO to overrun.
func (d *Device) ScanAnalogInRaw(ctx context.Context, cfg AnalogInScanConfig) iter.Seq2[[]uint32, error] {
	return func(yield func([]uint32, error) bool) {
		if err := d.ConfigureAnalogInScan(cfg.Channels); err != nil {
			yield(nil, err)
			return
		}

		if cfg.PacketSize == 0 {
			nCh := len(cfg.Channels)
			samplesPerScan := nCh
			if cfg.Rate*float64(samplesPerScan) < MaxPacketSize/4 {
				cfg.PacketSize = uint8(min(samplesPerScan, 256) - 1)
			}
		}

		// Pre-compute reader parameters before starting the scan to
		// minimize latency between scan start and first USB read.
		nCh := len(cfg.Channels)
		batch := scanBatchSize(cfg.Rate, nCh)
		batchBytes := batch * nCh * 4
		readTimeout := scanReadTimeout(cfg.Rate, batch)

		type readResult struct {
			buf []byte // full-capacity buffer (return to pool)
			n   int    // bytes actually read
			err error
		}

		depth := cfg.PipelineDepth
		if depth <= 0 {
			depth = DefaultPipelineDepth
		}

		ch := make(chan readResult, depth)
		stop := make(chan struct{})

		numReaders := DefaultConcurrentReaders
		poolSize := depth + numReaders
		free := make(chan []byte, poolSize)
		for range poolSize {
			free <- make([]byte, batchBytes)
		}

		_ = d.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)

		if err := d.StartAnalogInScan(cfg); err != nil {
			yield(nil, err)
			return
		}

		var readerWg sync.WaitGroup
		for range numReaders {
			readerWg.Add(1)
			go func() {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				defer readerWg.Done()

				for {
					select {
					case <-stop:
						return
					default:
					}

					var buf []byte
					select {
					case buf = <-free:
					default:
						buf = make([]byte, batchBytes)
					}

					n, err := d.transport.BulkReadInto(epBulkIn, buf, readTimeout)
					if err != nil && n == 0 {
						free <- buf
						if sd, se := d.transport.ControlIn(cmdStatus, 0, 0, 2); se == nil {
							if Status(wire.Uint16LE(sd)).AInScanOverrun() {
								_ = d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
								_ = d.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
								err = ErrScanOverrun
							}
						}
						select {
						case ch <- readResult{nil, 0, err}:
						case <-stop:
						}
						return
					}

					select {
					case ch <- readResult{buf, n, nil}:
					case <-stop:
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

		defer func() {
			close(stop)
			_ = d.StopAnalogInScan()
			for r := range ch {
				if r.buf != nil {
					free <- r.buf
				}
			}
		}()

		total := int(cfg.Count) // 0 = continuous
		scansRead := 0
		frame := make([]uint32, nCh)

		for result := range ch {
			if result.err != nil {
				yield(nil, result.err)
				return
			}
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			nSamples := result.n / 4
			nActual := nSamples / nCh
			for s := range nActual {
				for i := range nCh {
					off := (s*nCh + i) * 4
					frame[i] = wire.Uint32LE(result.buf[off : off+4])
				}
				if !yield(frame, nil) {
					free <- result.buf
					return
				}
				scansRead++
				if total > 0 && scansRead >= total {
					free <- result.buf
					return
				}
			}
			free <- result.buf
		}
	}
}

// ScanAnalogInBulk returns an iterator that yields raw USB bulk data as byte
// slices without per-frame unpacking. Each yielded slice contains one or more
// complete frames in little-endian uint32 format (nChannels × 4 bytes per
// frame). This is the highest-throughput path for raw captures where the data
// can be written directly to storage without decode/re-encode.
//
// The yielded slice is only valid until the next iteration.
//
// When cfg.Count is set, the last yielded slice may be truncated to contain
// only the remaining frames needed to reach the requested count.
func (d *Device) ScanAnalogInBulk(ctx context.Context, cfg AnalogInScanConfig) iter.Seq2[[]byte, error] {
	return func(yield func([]byte, error) bool) {
		if err := d.ConfigureAnalogInScan(cfg.Channels); err != nil {
			yield(nil, err)
			return
		}

		if cfg.PacketSize == 0 {
			nCh := len(cfg.Channels)
			samplesPerScan := nCh
			if cfg.Rate*float64(samplesPerScan) < MaxPacketSize/4 {
				cfg.PacketSize = uint8(min(samplesPerScan, 256) - 1)
			}
		}

		nCh := len(cfg.Channels)
		batch := scanBatchSize(cfg.Rate, nCh)
		batchBytes := batch * nCh * 4
		readTimeout := scanReadTimeout(cfg.Rate, batch)
		frameSize := nCh * 4

		type readResult struct {
			buf []byte // full-capacity buffer (return to pool via free channel)
			n   int    // bytes actually read
			err error
		}

		depth := cfg.PipelineDepth
		if depth <= 0 {
			depth = DefaultPipelineDepth
		}

		ch := make(chan readResult, depth)
		stop := make(chan struct{})

		// Pre-allocate a pool of read buffers sized for concurrent
		// readers plus pipeline depth.
		numReaders := DefaultConcurrentReaders
		poolSize := depth + numReaders
		free := make(chan []byte, poolSize)
		for range poolSize {
			free <- make([]byte, batchBytes)
		}

		// Clear FIFO before starting to avoid inheriting stale state
		// from a previous aborted scan.
		_ = d.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)

		if err := d.StartAnalogInScan(cfg); err != nil {
			yield(nil, err)
			return
		}

		// Launch multiple reader goroutines. Each goroutine keeps one
		// synchronous libusb_bulk_transfer in flight. With N goroutines,
		// the USB host controller always has N transfers queued — when
		// one completes, another is already pending, eliminating dead
		// time that would cause device FIFO overflow.
		var readerWg sync.WaitGroup
		for range numReaders {
			readerWg.Add(1)
			go func() {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				defer readerWg.Done()

				for {
					select {
					case <-stop:
						return
					default:
					}

					var buf []byte
					select {
					case buf = <-free:
					default:
						buf = make([]byte, batchBytes)
					}

					n, err := d.transport.BulkReadInto(epBulkIn, buf, readTimeout)
					if err != nil && n == 0 {
						free <- buf
						if sd, se := d.transport.ControlIn(cmdStatus, 0, 0, 2); se == nil {
							if Status(wire.Uint16LE(sd)).AInScanOverrun() {
								_ = d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
								_ = d.transport.ControlOut(cmdAInClearFIFO, 0, 0, nil)
								err = ErrScanOverrun
							}
						}
						select {
						case ch <- readResult{nil, 0, err}:
						case <-stop:
						}
						return
					}

					select {
					case ch <- readResult{buf, n, nil}:
					case <-stop:
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

		defer func() {
			close(stop)
			_ = d.StopAnalogInScan()
			for r := range ch {
				if r.buf != nil {
					free <- r.buf
				}
			}
		}()

		total := int(cfg.Count) // 0 = continuous
		scansRead := 0

		for result := range ch {
			if result.err != nil {
				yield(nil, result.err)
				return
			}
			if ctx.Err() != nil {
				yield(nil, ctx.Err())
				return
			}

			data := result.buf[:result.n]
			if total > 0 {
				remaining := (total - scansRead) * frameSize
				if len(data) > remaining {
					data = data[:remaining]
				}
			}

			if !yield(data, nil) {
				free <- result.buf
				return
			}

			free <- result.buf
			scansRead += len(data) / frameSize
			if total > 0 && scansRead >= total {
				return
			}
		}
	}
}
