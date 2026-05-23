# Phase 1 — Single-shot AOut + Paced Scan (PlayOnce / Loop)

## Goal

Land a complete analog-output API for the USB-1808/1808X: single-shot DAC
writes (volts + raw + helper), and a paced scan API supporting both finite
(`PlayOnce`) and continuous (`Loop`) playback. Build the host-side URB-ring +
refill engine that Phase 2's `Writer` will reuse. Mirror AIn's
overrun-tolerance pattern for underrun. Add zero-on-close safety.

## Scope

**Incremental change.** Builds on the existing v4 architecture (`device/`,
`transport/`, `wire/`). No package moves, no breaking API changes to AIn.

## Required skills

Read these before touching code; reread when uncertain:

- `~/.claude/skills/golang-code-style/` — applies to **every** Go file in
  this plan. Naming, control flow, error wrapping, comment policy, exported
  vs unexported boundaries.
- `~/.claude/skills/golang-logging/` — applies to any logging added to the
  AOut handle or refill goroutine (use `*slog.Logger` passed via the
  existing `Device.log` field; do not create new package-level loggers).
- `~/.claude/skills/golang-cli/` — applies to step 14 (`daq aout` subcommand)
  if that step is kept in Phase 1.
- `~/.claude/skills/makefiles/` — applies only if step 16 modifies the
  Makefile.

## Contracts

### Files created

```
device/
  scan_out_config.go       AOutScanConfig, AOutScanOption, validation, sentinel errors
  scan_out_handle.go       AOutScanHandle: lifecycle, refill loop, underrun handling
  scan_out_handle_test.go  table-driven mock-transport tests
  analog_out.go            single-shot AnalogOut, AnalogOutRaw, VoltsToAOutRaw
  analog_out_test.go       tests for single-shot
transport/
  bulk_out_ring.go         host-driven bulk-OUT URB ring (new file)
  bulk_out_ring_test.go    ring tests
docs/
  aout.md                  user-facing AOut guide
_examples/
  analog_out/main.go       single-shot DC level demo
  play_waveform/main.go    PlayOnce + Loop demo
```

### Files modified

```
device/
  constants.go             add epBulkOut = 0x02
  device.go                add zero-on-close behavior; KeepAOutOnClose option
  errors.go                add ErrAOutScanRunning, ErrAOutScanNotStarted,
                           ErrAOutScanStopped, ErrAOutUnderrun, ErrInvalidAOutQueue
  info.go                  remove AnalogOutRaw (moves to analog_out.go)
transport/
  transport.go             add AsyncBulkWriter optional interface
  libusb.go                implement AsyncBulkWriter (NewBulkOutRing)
  mock.go                  extend to satisfy AsyncBulkWriter for tests
docs/
  scanning-pipeline.md     add an output-pipeline section / mermaid diagram
  README.md                add link to aout.md
doc.go                     mention CreateAOutScan in quick-start
```

## Steps

### 1. Constants and errors

**Files:** `device/constants.go`, `device/errors.go`.

Add the bulk-OUT endpoint and the underrun-restart safety-valve constant:

```go
// device/constants.go
const epBulkOut = 0x02

const maxAOutUnderrunsPerSecond = 3 // mirrors maxOverrunsPerSecond
const aoutRingTransferCount = 32     // mirrors ringTransferCount
const aoutRingMaxStageSize = 16 * 1024 // mirrors ringMaxStageSize (xhci safe ≤ 16 KiB)
```

Add error sentinels in `device/errors.go`:

```go
ErrAOutScanRunning    = errors.New("usb1808: analog output scan already running")
ErrAOutScanNotStarted = errors.New("usb1808: analog output scan not started")
ErrAOutScanStopped    = errors.New("usb1808: analog output scan already stopped")
ErrAOutUnderrun       = errors.New("usb1808: analog output FIFO underrun")
ErrInvalidAOutQueue   = errors.New("usb1808: invalid AOut scan queue")
```

**Verify:** `go build ./...`.

### 2. Bulk-OUT URB ring in transport

**Files:** `transport/bulk_out_ring.go` (new), `transport/transport.go`,
`transport/libusb.go`, `transport/mock.go`,
`transport/bulk_out_ring_test.go` (new).

This is the trickiest piece of Phase 1. The bulk-OUT ring is structurally
similar to the existing `bulkRing` in `transport/ring.go` but **inverted**:

- The existing in-ring auto-resubmits on completion (the device keeps
  producing data; we just keep re-arming the read transfers).
- The out-ring needs **host-driven submission**: each slot has to be filled
  with fresh sample data *before* it is submitted. On completion we mark the
  slot free and signal a "refill" goroutine; the refill goroutine pulls
  bytes from a source (`io.Reader`-like) into the next free slot and submits.

#### Public interface

```go
// transport/transport.go — add alongside AsyncBulkReader
type AsyncBulkWriter interface {
    NewBulkOutRing(endpoint uint8, bufSize, count int, source BulkOutSource,
                   timeoutMs uint) (BulkOutRing, error)
}

// BulkOutSource fills the next staging buffer with bytes to send.
// Implementations must return io.EOF when the source is exhausted; any other
// non-nil error aborts the ring. Returning (0, nil) is a transient "no data
// yet" signal — the ring will retry, applying backpressure to the caller.
type BulkOutSource interface {
    Fill(buf []byte) (n int, err error)
}

type BulkOutRing interface {
    // Done closes when the source returns io.EOF AND all in-flight transfers
    // have completed.
    Done() <-chan struct{}
    // Err returns any error from the source or libusb after Done is closed.
    Err() error
    // Stop cancels in-flight transfers and frees C resources.
    Stop()
    // Stats returns lifetime counters (Submitted, Completed, BytesSent).
    Stats() BulkOutRingStats
}
```

#### Implementation notes

In `transport/bulk_out_ring.go`:

- Mirror `transport/ring.go`'s cgo callback wiring exactly — same
  `goRingCallback` trampoline pattern, `cgo.Handle`, locked-OS-thread event
  loop. The transfer struct, allocation, and cleanup are the same shape.
- Each transfer's C buffer is sized to `bufSize`. The constructor pre-fills
  all `count` buffers by calling `source.Fill` for each, then submits them
  all. If any fill returns `io.EOF` before the ring is full, the ring still
  starts (only those that were filled are submitted; the remainder are kept
  free).
- The completion callback `onTransferComplete`:
  - On success: mark slot free, signal the refill goroutine.
  - On `LIBUSB_TRANSFER_CANCELLED` or stop: decrement pending; if pending=0,
    close `done`.
  - On other error: record err, stop the ring.
- A single refill goroutine (separate from the libusb event loop) listens on
  a "slot free" notification channel, pulls bytes from `source.Fill` until
  the slot is filled, then submits the transfer. This keeps `Fill` calls
  out of the libusb event-loop thread (important: `Fill` may block on a
  channel/Write call from caller code).
- **ZLP handling:** if a transfer's payload length is a multiple of 512 (the
  HS bulk wMaxPacketSize), set `LIBUSB_TRANSFER_ADD_ZERO_PACKET` flag on
  that transfer. libusb 1.0.9+ supports this; otherwise emit an extra
  zero-length transfer after. Document the libusb version requirement in
  `transport/bulk_out_ring.go`.
- **Termination semantics:** when `source.Fill` returns `io.EOF`, the refill
  goroutine stops submitting new transfers and waits for the remaining
  in-flight ones to complete (drain). Only then does `Done` close. This is
  what makes `Writer.Close()` (Phase 2) a drain operation.
- **Short fills:** if `Fill` returns `(n, nil)` with `n < len(buf)`, submit
  the partial transfer (truncated to `n` bytes). This is needed for the
  "last buffer" case at end-of-source.

#### Mock

`transport/mock.go` must satisfy `AsyncBulkWriter`. The mock keeps a slice
of `Fill` results to deliver and records the bytes that were "submitted".

#### Tests

`transport/bulk_out_ring_test.go`:

- Fill a constant pattern → confirm the bytes the source produced are the
  bytes that would have been sent.
- Short-fill at end → confirm partial transfer behavior and clean Done.
- Source returns `(0, nil)` then real data → confirm backpressure-like
  retry behavior with no spinning.
- Source returns error → confirm Err propagates and Done closes.
- Cancel via Stop while transfers in flight → no leaks.

Note: real libusb-backed tests require hardware; mock is sufficient for unit
testing the ring logic above the cgo boundary. Use the same mock-then-cgo
split as `transport/ring.go` is tested with (no direct ring tests; verified
through `device/scan_handle_test.go`).

**Verify:** `go test ./transport/...`, `make vet staticcheck`.

### 3. Single-shot AOut

**Files:** `device/analog_out.go` (new), `device/info.go` (remove
`AnalogOutRaw`), `device/analog_out_test.go` (new).

```go
// device/analog_out.go
package device

import (
    "fmt"
    "math"
)

// AnalogOut writes a calibrated voltage to the specified DAC channel.
// Refuses with ErrAOutScanRunning if an output scan is active.
func (d *Device) AnalogOut(channel int, volts float64) error {
    if channel < 0 || channel >= NumAOutChannels {
        return fmt.Errorf("%w: %d", ErrInvalidChannel, channel)
    }
    code := d.VoltsToAOutRaw(channel, volts)
    return d.AnalogOutRaw(channel, code)
}

// AnalogOutRaw writes a raw 16-bit DAC code (offset binary).
func (d *Device) AnalogOutRaw(channel int, value uint16) error {
    if channel < 0 || channel >= NumAOutChannels {
        return fmt.Errorf("%w: %d", ErrInvalidChannel, channel)
    }
    d.mu.Lock()
    defer d.mu.Unlock()
    st, err := d.status()
    if err != nil {
        return err
    }
    if st.AOutScanRunning() {
        return ErrAOutScanRunning
    }
    return d.transport.ControlOut(cmdAOut, uint16(value), uint16(channel), nil)
}

// VoltsToAOutRaw converts volts to a calibrated 16-bit DAC code.
func (d *Device) VoltsToAOutRaw(channel int, volts float64) uint16 {
    // Pre-calibration: raw = volts/10 * 32768 + 32768
    raw := volts/10.0*32768.0 + 32768.0
    cal := raw*float64(d.calAOut[channel].Slope) + float64(d.calAOut[channel].Offset)
    cal = math.Round(cal)
    if cal < 0 {
        cal = 0
    }
    if cal > 65535 {
        cal = 65535
    }
    return uint16(cal)
}
```

`device/info.go`: remove the existing `AnalogOutRaw` (move to
`analog_out.go`).

Tests use `transport.MockTransport` to capture the `ControlOut` call and
verify `wValue` (DAC code) and `wIndex` (channel). Add a test case where the
status response has `AOutScanRunning` set, and verify
`ErrAOutScanRunning`.

**Verify:** `go test ./device/...`.

### 4. AOut scan configuration and validation

**Files:** `device/scan_out_config.go` (new).

```go
package device

import "fmt"

type AOutScanConfig struct {
    Channels []int   // queue values, each 0 or 1
    Rate     int     // Hz per channel
    Count    uint32  // 0 = continuous (loop forever)
    Options  uint8   // bit 0 = ext trigger, bit 1 = pattern, bit 2 = retrigger
}

type AOutScanOption func(*aoutScanOptions)

type UnderrunFunc func(framesPlayed uint64, ts time.Time)

type aoutScanOptions struct {
    pipelineDepth    int
    onUnderrun       UnderrunFunc
    stopOnUnderrun   bool
}

const DefaultAOutPipelineDepth = 32

func WithAOutPipelineDepth(n int) AOutScanOption {
    return func(o *aoutScanOptions) {
        if n > 0 {
            o.pipelineDepth = n
        }
    }
}

func WithUnderrunCallback(fn UnderrunFunc) AOutScanOption {
    return func(o *aoutScanOptions) { o.onUnderrun = fn }
}

func WithStopOnUnderrun() AOutScanOption {
    return func(o *aoutScanOptions) { o.stopOnUnderrun = true }
}

func validateAOutScanConfig(cfg AOutScanConfig, model Model) error {
    if len(cfg.Channels) == 0 || len(cfg.Channels) > MaxAOutQueue {
        return fmt.Errorf("%w: queue length %d (max %d)",
            ErrInvalidAOutQueue, len(cfg.Channels), MaxAOutQueue)
    }
    seen := map[int]bool{}
    for _, ch := range cfg.Channels {
        if ch < 0 || ch >= NumAOutChannels {
            return fmt.Errorf("%w: channel %d", ErrInvalidChannel, ch)
        }
        if seen[ch] {
            return fmt.Errorf("%w: channel %d appears twice", ErrInvalidAOutQueue, ch)
        }
        seen[ch] = true
    }
    if cfg.Rate <= 0 {
        return fmt.Errorf("usb1808: invalid AOut scan rate: %d", cfg.Rate)
    }
    maxRate := 125_000
    if model == USB1808X {
        maxRate = 500_000
    }
    if cfg.Rate > maxRate {
        return fmt.Errorf("usb1808: AOut rate %d exceeds model max %d", cfg.Rate, maxRate)
    }
    return nil
}
```

Tests cover boundary cases on length, duplicate channels, rate vs model.

**Verify:** `go test ./device/...`.

### 5. AOutScanHandle skeleton

**Files:** `device/scan_out_handle.go` (new).

The handle owns:

- The validated config + options
- A staging source (`samplesSource` — for Phase 1 either a fixed buffer
  played once, or the same buffer cycled forever)
- Lifecycle channels: `stop`, `done`
- A reference to a `transport.BulkOutRing` (replaced on underrun recovery)
- An error slot, scan-state enum, and `stopOnce`
- An optional `time.Timer` watchdog for finite scans

```go
type AOutScanHandle struct {
    dev    *Device
    cfg    AOutScanConfig
    opts   aoutScanOptions
    src    samplesSource

    frameSize int
    bufSize   int  // per URB

    stop, done chan struct{}
    state      scanState
    stopOnce   sync.Once
    err        error
    mu         sync.Mutex

    aw   transport.AsyncBulkWriter
    ring transport.BulkOutRing
}
```

`CreateAOutScan(cfg, opts...)`:

1. Validate config.
2. Write `cmdADCSetup`-equivalent — none for AOut; skip.
3. Configure scan queue via `cmdAOutScanConf`. Payload is 3 bytes: pad
   unused queue slots with `0` (it doesn't matter, only `lastChan-1` slots
   are read). `wIndex = lastChan - 1 = len(cfg.Channels)-1`.
4. Build the handle struct; do not yet preload data or start the scan.

`Start()`:

1. CAS state `scanIdle → scanRunning`.
2. Clear the AOut FIFO (`cmdAOutClearFIFO`).
3. Compute `bufSize` via `aoutRingStageSize(rate, nCh)` — same shape as
   `ringStageSize` (10 ms target, capped at 16 KiB, packet-aligned).
4. Get `AsyncBulkWriter` from transport (`d.transport.(transport.AsyncBulkWriter)`).
   If not supported, return error.
5. Create the bulk-OUT ring with the handle's `samplesSource`. Submitting
   `count` pre-filled buffers preloads the device FIFO.
6. Send `cmdAOutScanStart` with the 13-byte payload (`wire.AOutScanStartPayload`).
7. Launch the reader/monitor goroutine (`runAOutLoop`).

`Stop()`:

1. CAS state to `scanStopped`.
2. `stopOnce.Do(close(h.stop))`.
3. Send `cmdAOutScanStop` and `cmdAOutClearFIFO`.
4. `<- h.done`; return `h.Err()`.

`Wait(ctx)`:

- Block until `h.done` closes OR `ctx.Done()` fires.
- For `Count == 0` (continuous loop), `Wait` never returns naturally; users
  must call `Stop`. Document this.

`Err()`: same shape as `ScanHandle.Err()`.

### 6. The refill loop + samplesSource

**Files:** `device/scan_out_handle.go` continued, `device/scan_out_source.go`
(or co-located).

```go
// samplesSource produces sample bytes for the bulk-OUT ring.
// Implementations:
//   *bufferOnceSource — plays a fixed []byte once then EOFs (PlayOnce)
//   *bufferLoopSource — plays a fixed []byte cyclically (Loop)
//   *writerSource     — fed by AOutWriter.Write (Phase 2)
type samplesSource interface {
    // Fill copies up to len(buf) bytes into buf. Returns (n, nil) on
    // progress, (n, io.EOF) when exhausted (n may be 0), or
    // (n, err) on fatal error.
    Fill(buf []byte) (int, error)
}

// bufferOnceSource: linear read-once.
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

// bufferLoopSource: read-cyclically.
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
```

Tests verify Fill semantics for each source.

### 7. The runAOutLoop goroutine

**Files:** `device/scan_out_handle.go` continued.

Mirror `runAsyncReader` for AIn, but inverted for output. Pseudocode:

```go
func (h *AOutScanHandle) runAOutLoop() {
    defer close(h.done)
    defer func() { h.stopOnce.Do(func() { close(h.stop) }) }()

    // Monitor for stop: send scanStop + clearFIFO + ring.Stop.
    go func() {
        <-h.stop
        h.dev.mu.Lock()
        _ = h.dev.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
        _ = h.dev.transport.ControlOut(cmdAOutClearFIFO, 0, 0, nil)
        h.dev.mu.Unlock()
        h.ring.Stop()
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
        // Wait for the ring to finish (source EOF + drain) OR an underrun.
        select {
        case <-h.ring.Done():
            // Source exhausted cleanly. Check Err.
            if err := h.ring.Err(); err != nil {
                h.setErr(err)
            }
            return
        case <-h.stop:
            return
        case <-h.underrunCh():
            // h.underrunCh fires when the monitor goroutine (below) detects
            // statusAOutScanUnder. The detector polls status periodically
            // and also checks on each ring error.
            now := time.Now()
            if h.opts.stopOnUnderrun {
                h.setErr(ErrAOutUnderrun)
                return
            }
            if recentUnderruns > 0 && now.Sub(firstUnderrunTime) < time.Second {
                recentUnderruns++
                if recentUnderruns >= maxAOutUnderrunsPerSecond {
                    h.setErr(fmt.Errorf("%w: %d underruns in %v",
                        ErrAOutUnderrun, recentUnderruns,
                        now.Sub(firstUnderrunTime)))
                    return
                }
            } else {
                recentUnderruns = 1
                firstUnderrunTime = now
            }
            if h.opts.onUnderrun != nil {
                h.opts.onUnderrun(h.framesPlayed(), now)
            }
            // Restart: stop scan, clear FIFO, restart ring + scan.
            if err := h.restartAOutScan(); err != nil {
                h.setErr(fmt.Errorf("restart after underrun: %w", err))
                return
            }
        }
    }
}
```

**Underrun detection design.** Three signals can indicate an underrun:

1. `statusAOutScanUnder` bit set in the device status register.
2. A bulk-OUT transfer completing with `LIBUSB_TRANSFER_ERROR` or a stall.
3. The source returning `io.EOF` while the device is still expected to be
   running (e.g. for an "infinite loop" source that suddenly stops).

For Phase 1 the simplest correct approach: poll `status()` on a 10 ms ticker
inside `runAOutLoop`, and on each tick check `AOutScanUnderrun()`. When the
bit is set, signal underrun. **Open question:** is the bit sticky (latched
until read or cleared)? Verify against hardware in step 12 before locking
in the polling design. uldaq does not poll for underrun — it relies on the
host-side accounting of "bytes written vs expected at this elapsed time."
We may want to switch to that approach if the status bit proves unreliable.

`restartAOutScan` must:

1. Stop ring (already done by `drainRing`-equivalent).
2. Send `cmdAOutScanStop`.
3. Send `cmdAOutClearFIFO`.
4. Reset the source's playback pointer if it's a `bufferLoopSource` (so the
   restart begins at the start of the loop, not mid-cycle — debatable;
   alternative is to resume where the underrun happened). Recommend
   resetting; document the trade-off.
5. Create a new bulk-OUT ring with the same parameters.
6. Send `cmdAOutScanStart` with the same payload.

### 8. Convenience constructors

**Files:** `device/scan_out_handle.go` continued.

```go
func (d *Device) PlayOnce(channels []int, rate int, samples []float64) (*AOutScanHandle, error) {
    raw := make([]uint16, len(samples))
    for i, v := range samples {
        // Per-sample channel index modulo len(channels).
        raw[i] = d.VoltsToAOutRaw(channels[i%len(channels)], v)
    }
    return d.PlayOnceRaw(channels, rate, raw)
}

func (d *Device) PlayOnceRaw(channels []int, rate int, samples []uint16) (*AOutScanHandle, error) {
    cfg := AOutScanConfig{
        Channels: channels,
        Rate:     rate,
        Count:    uint32(len(samples) / len(channels)),
    }
    h, err := d.CreateAOutScan(cfg)
    if err != nil {
        return nil, err
    }
    data := uint16sToBytes(samples) // little-endian pack
    h.src = &bufferOnceSource{data: data}
    if err := h.Start(); err != nil {
        return nil, err
    }
    return h, nil
}

// Loop / LoopRaw are similar but Count=0 and source is bufferLoopSource.
```

Helper `uint16sToBytes` lives in `wire/` (mirroring existing
`PutUint32LE`-style helpers).

### 9. Zero-on-close

**Files:** `device/device.go`.

Add a `keepAOutOnClose bool` field to `Device`. Add an `Open` option:

```go
type Option func(*Device)

func KeepAOutOnClose() Option {
    return func(d *Device) { d.keepAOutOnClose = true }
}

func Open(opts ...Option) (*Device, error) { ... }
func OpenModel(model Model, opts ...Option) (*Device, error) { ... }
```

In `Device.Close()`:

```go
if d.initialized.Load() && !d.keepAOutOnClose {
    // Best-effort: write 0 V to both DACs. Ignore errors (we're closing).
    for ch := 0; ch < NumAOutChannels; ch++ {
        code := d.VoltsToAOutRaw(ch, 0.0)
        _ = d.transport.ControlOut(cmdAOut, uint16(code), uint16(ch), nil)
    }
}
// existing cleanup ...
```

**Note:** calibration table must already be built (`Init` called) for the
volts→code conversion to be correct. If `Close` is called before `Init`,
skip the zero-out (which the `initialized.Load()` guard handles).

**Verify:** `go test ./device/...` covering both Close paths.

### 10. Status monitoring

**Files:** `device/scan_out_handle.go`.

Add a periodic poll goroutine to monitor `AOutScanUnderrun()` and
`AOutScanDone()`:

```go
go func() {
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
```

**Verify:** `go test ./device/...` with a mock that flips the underrun bit
after N polls.

### 11. Tests with mock transport

**Files:** `device/scan_out_handle_test.go` (new), reuse the
`overrunMockTransport` pattern from `scan_handle_test.go`.

Test cases:

- `Create + Start + Stop` lifecycle, verify control calls (`cmdAOutScanConf`,
  `cmdAOutClearFIFO`, `cmdAOutScanStart`, `cmdAOutScanStop`).
- `PlayOnce` with a small buffer: verify the bytes the mock received match
  the input samples encoded as `uint16` LE.
- `Loop` with a small buffer: verify cyclic reads and that `Stop` works.
- Underrun event: mock toggles `statusAOutScanUnder` on second status read;
  verify `onUnderrun` callback fires, scan auto-restarts, and a normal
  completion follows.
- Underrun storm: mock holds the bit set; verify `ErrAOutUnderrun` surfaces
  after `maxAOutUnderrunsPerSecond` events.
- `WithStopOnUnderrun()`: verify fail-fast on first underrun.
- Invalid configs: empty queue, oversize queue, duplicate channels, bad
  rate, rate > model max.
- Single-shot while scan running: verify `ErrAOutScanRunning`.

All tests use `mockRingTransport` extended with `NewBulkOutRing`. Conversion
helpers between bytes and `[]uint16` go in `wire/` and have their own
tests.

**Verify:** `make test`.

### 12. Hardware integration tests (optional / opt-in)

**Files:** `device/scan_out_hwtest.go` with `//go:build hardware` tag (or
similar).

Real-hardware tests confirm:

- Single-shot at 0 V, +5 V, −5 V → measured against actual DAC output if a
  loopback to AIn is wired (1808X has DAC0→AIn0 internal calibration loop?
  check spec; otherwise external loopback).
- `PlayOnce` of a 1 kHz sine for 100 ms at 100 kS/s: confirm device returns
  `AOutScanDone` after expected duration.
- `Loop` of a 100-sample triangle wave at 10 kS/s: confirm continuous
  output via loopback measurement for 5 seconds.
- Resolve the **open question** from step 7: does
  `statusAOutScanUnder` latch or self-clear?

Document how to run: `go test -tags hardware ./device/...`.

**Verify:** manual run on a USB-1808X.

### 13. Examples

**Files:** `_examples/analog_out/main.go`, `_examples/play_waveform/main.go`.

`_examples/analog_out/main.go`: Open device, Init, AnalogOut a ramp from
−10 V to +10 V in 1 V steps with 200 ms between steps.

`_examples/play_waveform/main.go`: Open device, Init, generate a 1 kHz sine
table of one period (say 100 samples at 100 kS/s), call `Loop`, wait 3
seconds, call `Stop`.

Make sure both examples follow the existing structure (look at
`_examples/streaming/main.go`).

### 14. CLI subcommands

**Files:** `cmd/daq/cmd_aout.go`.

Add `daq aout set --channel N --volts V` for single-shot, and `daq aout
play --channels 0,1 --rate 100k --duration 10s --sine 1k` (or similar) for
scan-based testing. Hook into `cmd/daq/main.go`'s Kong CLI structure (see
existing `cmd/daq/cmd_capture.go` for the pattern). This is optional for
Phase 1 — can be deferred to Phase 2 if scope feels too big.

### 15. Documentation

**Files:** `docs/aout.md` (new), `docs/scanning-pipeline.md` (extend),
`docs/README.md`, root `README.md`, `doc.go`.

`docs/aout.md` covers:

- Overview (2 DACs, ±10 V, fixed range, max rate per model)
- Single-shot usage (volts + raw)
- Paced scan: PlayOnce + Loop with worked examples
- Underrun behavior + callback + safety valve + WithStopOnUnderrun
- Calibration warm-up note (15 min recommended)
- DAC holds last value on power down; default zero-on-close behavior;
  KeepAOutOnClose escape hatch
- Pointer to docs/sync.md (Phase 2) for AI/AO sync
- Known limits: no DIO-in-AOut-scan yet, no FIFO-fill query, 8 KiB device
  FIFO

`docs/scanning-pipeline.md` gets a sibling "Output Pipeline" section with a
mermaid diagram showing:

```
source → refill goroutine → URB ring → libusb event loop → EP2 OUT → device FIFO → DAC
                                                                       ↑
                                                                    pacer
```

Update `docs/README.md` and the root `README.md` to list `aout.md` under
the docs index, and add a short AOut snippet to the README quick-start.

### 16. Final verification

```bash
make fmt vet lint staticcheck gosec test build
```

Then run the two new examples on real hardware (or in `make integration`
if such a target exists; otherwise document manual steps).

## Dependencies

None. All work uses the existing libusb cgo bindings and stdlib.

## Open Questions

1. **`statusAOutScanUnder` latch behavior.** Does the underrun bit stick
   until read/cleared, or does it self-clear? Affects how often we need to
   poll. Resolve in step 12 against hardware.
2. **ZLP via `LIBUSB_TRANSFER_ADD_ZERO_PACKET` vs explicit zero-length
   transfer.** Confirm the libusb version we link against supports the
   flag; otherwise the ring needs to submit an explicit ZLP after any
   exact-multiple-of-512 transfer.
3. **Loop-source restart policy on underrun.** Plan recommends resetting to
   the start of the buffer on restart. Confirm during step 12; alternative
   is resuming from the underrun offset (probably wrong because the device
   will glitch on resume regardless).
4. **Does the device require a minimum number of preloaded samples before
   `cmdAOutScanStart`?** uldaq preloads "at least one URB" before starting.
   wjasper's test preloads ~1 KB. Investigate during step 12 — may need to
   make ring start blocking-wait-until-first-fill-completes.
5. **CLI scope:** include `daq aout` in Phase 1 (step 14) or push to a
   later phase? Resolve before starting step 14.
