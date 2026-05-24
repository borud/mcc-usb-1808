# Phase 2 — Streaming Writer + Sync Example

## Goal

Expose a `Writer`-shaped API for continuous analog output that lets callers
push samples synchronously at their own pace, with backpressure flowing from
the device FIFO through libusb back to the calling goroutine. Add a worked
example for AI/AO synchronization. Round out user-facing documentation.

## Scope

**Incremental change.** Builds entirely on the Phase 1 engine. No type or
contract changes to Phase 1; `AOutWriter` is an alternative driver for the
same `samplesSource` / `transport.BulkOutRing` machinery.

## Required skills

Read these before touching code; reread when uncertain:

- `~/.claude/skills/golang-code-style/` — every Go file in this phase.
- `~/.claude/skills/golang-logging/` — any logging in `AOutWriter` or the
  sync example.
- `~/.claude/skills/makefiles/` — only if step 8 modifies the Makefile.

## Contracts

### Files created

```
device/
  scan_out_writer.go       AOutWriter type + lifecycle
  scan_out_writer_test.go  Writer tests with mock transport
docs/
  sync.md                  AI/AO synchronization guide
_examples/
  synced_scan/main.go      AI + AO running off shared trigger + clock
  stream_aout/main.go      AOutWriter fed from a WAV-like file
```

### Files modified

```
docs/README.md             link sync.md
docs/aout.md               add Writer section
doc.go                     mention NewAOutWriter
```

## Contracts

### Public API

```go
// device/scan_out_writer.go
type AOutWriter struct { /* opaque */ }

func (d *Device) NewAOutWriter(cfg AOutScanConfig, opts ...AOutScanOption) (*AOutWriter, error)

// Start arms the scan but does not begin output until at least one full
// URB is preloaded (the device will not begin pacing until its FIFO has
// data). Implementations may block here until the ring is primed.
func (w *AOutWriter) Start() error

// Write copies samples into the next free URB slot and submits it. It
// blocks while no slot is free (backpressure). Returns the number of
// uint16 samples accepted (always equal to len(samples) on success).
func (w *AOutWriter) Write(samples []uint16) (int, error)

// Close signals end-of-stream. Pending URBs drain to the device; the
// scan stops on its own when the device FIFO empties. Close blocks
// until that drain completes.
func (w *AOutWriter) Close() error

// Abort cancels in-flight transfers, stops the scan, and clears the FIFO.
// Returns immediately.
func (w *AOutWriter) Abort() error

// Err returns any error that occurred during writing.
func (w *AOutWriter) Err() error
```

### Internal architecture

`AOutWriter` is a thin shell around the Phase 1 engine. The writer
implements a `samplesSource` whose `Fill` method blocks on a single-slot
"current chunk" channel:

```go
type writerSource struct {
    // pending receives []uint16 chunks from Write.
    pending chan []uint16
    // remainder of an in-flight chunk that didn't fit in one Fill call.
    leftover []uint16
    closed   atomic.Bool
}

func (s *writerSource) Fill(buf []byte) (int, error) {
    // Drain leftover first.
    if len(s.leftover) > 0 {
        n := writeSamplesLE(buf, s.leftover)
        s.leftover = s.leftover[n/2:]
        return n, nil
    }
    // Block for next chunk.
    chunk, ok := <-s.pending
    if !ok {
        return 0, io.EOF
    }
    n := writeSamplesLE(buf, chunk)
    if n < len(chunk)*2 {
        s.leftover = chunk[n/2:]
    }
    return n, nil
}
```

`Write` blocks by sending to `s.pending`. Backpressure shape:

- The bulk-OUT ring has N URB slots (default 32, like AIn).
- Each slot is `bufSize` bytes (~10 ms of audio at the configured rate).
- The refill goroutine inside the ring pulls from `writerSource.Fill` once
  per completed transfer.
- `pending` is unbuffered (or capacity 1) so `Write` blocks until the
  refill goroutine has consumed the previous chunk.
- Net effect: at steady state, the caller's `Write` rate matches the
  device's consumption rate. Faster callers stall; slower callers underrun
  → underrun policy from Phase 1 kicks in.

`Close` closes `s.pending`, which causes `Fill` to return `io.EOF`. The
ring drains remaining transfers; once `ring.Done()` fires, `Close` returns.

`Abort` triggers the handle's `stop` channel, which cascades into ring
stop, scan stop, FIFO clear.

## Steps

### 1. Lift the engine into something Writer-shareable

**Files:** `device/scan_out_handle.go`.

If Phase 1 hard-coded the `samplesSource` to `bufferOnceSource` /
`bufferLoopSource` directly on the handle struct, refactor so the handle
accepts any `samplesSource` and stores it as an interface field. Should
have been the design from the start; tighten if needed.

**Verify:** `go test ./device/...` (all Phase 1 tests still pass).

### 2. AOutWriter type

**Files:** `device/scan_out_writer.go` (new).

```go
type AOutWriter struct {
    h    *AOutScanHandle
    src  *writerSource
    once sync.Once
}

func (d *Device) NewAOutWriter(cfg AOutScanConfig, opts ...AOutScanOption) (*AOutWriter, error) {
    // For NewAOutWriter, override the source to writerSource.
    // Cfg.Count must be 0 (continuous) for Writer use — validate.
    if cfg.Count != 0 {
        return nil, fmt.Errorf("AOutWriter requires continuous mode (Count=0)")
    }
    h, err := d.CreateAOutScan(cfg, opts...)
    if err != nil {
        return nil, err
    }
    src := &writerSource{pending: make(chan []uint16)}
    h.src = src
    return &AOutWriter{h: h, src: src}, nil
}

func (w *AOutWriter) Start() error          { return w.h.Start() }
func (w *AOutWriter) Write(s []uint16) (int, error) { /* ... */ }
func (w *AOutWriter) Close() error          { /* close src; wait for done */ }
func (w *AOutWriter) Abort() error          { return w.h.Stop() }
func (w *AOutWriter) Err() error            { return w.h.Err() }
```

### 3. `writerSource` implementation

**Files:** `device/scan_out_writer.go` continued.

Details in the Contracts section. Key edge cases:

- `Write` called after `Close`: return `io.ErrClosedPipe`.
- `Write` called before `Start`: behavior must be defined. Recommend
  allowing it (just buffer through the source channel; the ring won't
  consume until `Start` runs).
- `Write` with `len(samples) > bufSize/2`: still works because `Fill`
  copies up to `len(buf)` and stashes the rest in `leftover`.
- Concurrent `Write` calls: serialize with a mutex. Document non-concurrent
  expectation, but enforce.

`writeSamplesLE` lives in `wire/` if not already present (pair with
`uint16sToBytes`).

### 4. Tests

**Files:** `device/scan_out_writer_test.go` (new).

- Open → NewAOutWriter → Start → Write small buffer → Close drains.
- Backpressure: configure ring depth 2 with small bufSize, write more than
  can fit, verify Write blocks until consumer drains.
- Abort while blocked in Write: Write returns an error, Abort cleans up.
- Underrun during Writer use: source starves (caller stops calling Write),
  Phase 1 underrun policy fires.
- Close before Start: confirm clean shutdown.
- Multiple Writes interleaved with one consumer: verify total bytes written
  equal total bytes consumed.

### 5. `_examples/stream_aout`

**Files:** `_examples/stream_aout/main.go` (new).

Demonstrate `io.Copy`-style usage: read a file of `uint16` LE samples and
stream to the DAC. Show how to wrap with a custom rate-limited reader if
the file shouldn't be played as fast as USB allows.

```go
file, _ := os.Open("waveform.raw")
defer file.Close()
buf := make([]uint16, 2048)
w, _ := dev.NewAOutWriter(cfg)
defer w.Close()
w.Start()
for {
    n, err := readUint16LE(file, buf)
    if n > 0 { w.Write(buf[:n]) }
    if err != nil { break }
}
```

### 6. AI/AO sync example

**Files:** `_examples/synced_scan/main.go` (new), `docs/sync.md` (new).

`docs/sync.md` explains:

- Trigger options: `ScanOptExternalTrigger` on both AIn and AOut, plus
  `dev.SetTriggerConfig(...)` to choose edge/level and polarity.
- Pacer sharing: physical jumper wire from ICLKO (pin) to OCLKI (pin) on
  the screw terminal block. AOut config sets `Rate` to whatever AIn is
  using; internally the AOut pacer is then driven externally because
  `pacer_period = 0`. **Confirm** this requires a config flag versus
  inferring from rate=0.
- Recommended start order: configure AOut + preload FIFO + ScanStart
  AOut (it waits on trigger); then ScanStart AIn similarly; then issue
  the trigger (external pulse or software).
- Caveats: shared trigger config register means you cannot use one
  trigger style for AIn and a different style for AOut.

`_examples/synced_scan/main.go` shows the full recipe with a 1 kHz sine
stimulus on DAC0 and 4 AIn channels recording the response at 100 kS/s for
1 second. Use a software trigger for portability (no jumper wire needed
for the example to "work"; document that a hardware jumper improves
phase alignment).

### 7. Docs cleanup

**Files:** `docs/aout.md`, `docs/README.md`, root `README.md`.

- `docs/aout.md`: add a "Streaming with AOutWriter" section after the
  PlayOnce/Loop sections. Show the `io.Copy`-style snippet.
- `docs/README.md`: index `sync.md`.
- Root `README.md`: add NewAOutWriter to the quick-start? Optional.

### 8. Final verification

```bash
make fmt vet lint staticcheck gosec test build
```

Plus manual run of `_examples/stream_aout` and `_examples/synced_scan` on
real hardware.

## Dependencies

None.

## Open Questions

1. **External pacer flag.** Does setting `pacer_period = 0` in
   `cmdAOutScanStart` require a separate option byte bit? wjasper/uldaq
   source suggests not — just send 0 — but verify.
2. **Sync example mode.** External hardware trigger requires a physical
   pulse generator; software trigger is documented but the latency is
   non-deterministic. Decide which mode the example uses by default.
3. **`AOutWriter.Write` concurrency.** Plan says serialize with a mutex.
   Confirm that's acceptable or whether to refuse with a typed error if
   concurrent Writes are detected.
4. **Drain timeout.** `Close()` blocks until the device finishes the FIFO.
   If the device is in a weird state (status bit stuck), Close could hang
   forever. Need a watchdog inside Close — say, the configured rate plus
   100 ms grace. Mirror the AIn finite-scan watchdog from
   `runAsyncReader`.
