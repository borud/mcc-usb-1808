# Analog Output (AOut)

The USB-1808 and USB-1808X provide two 16-bit DAC channels (DAC0, DAC1) with
a fixed ±10 V output range and a maximum output rate of 125 kS/s (1808) or
500 kS/s (1808X) per channel.

## DAC code format

Samples are offset binary `uint16` values:

| Code     | Voltage |
|----------|---------|
| `0x0000` | −10 V   |
| `0x8000` | 0 V     |
| `0xFFFF` | +10 V   |

## Single-shot output

```go
// Calibrated voltage write (blocks while scan active).
err := dev.AnalogOut(0, 2.5)        // DAC0 → 2.5 V
err  = dev.AnalogOut(1, -5.0)       // DAC1 → −5 V

// Raw DAC code write.
err = dev.AnalogOutRaw(0, 0x8000)   // DAC0 → 0 V

// Voltage-to-code conversion (uses EEPROM calibration coefficients).
code := dev.VoltsToAOutRaw(0, 3.3)
```

Single-shot writes are refused with `ErrAOutScanRunning` while a paced scan
is active. There is no implicit stop; call `h.Stop()` first.

## Paced scan

### PlayOnce — finite waveform

```go
// One period of a 1 kHz triangle wave at 10 kS/s on DAC0.
samples := makeSineTable(10000, 1000) // 10 samples
h, err := dev.PlayOnce([]int{0}, 10_000, samples)
if err != nil { ... }
// Wait for the scan to finish.
if err := h.Wait(ctx); err != nil { ... }
```

### Loop — continuous waveform

```go
h, err := dev.Loop([]int{0}, 10_000, samples)
if err != nil { ... }
time.Sleep(5 * time.Second)
h.Stop()
```

### Raw variants

Use `PlayOnceRaw` and `LoopRaw` to provide pre-encoded `[]uint16` samples and
avoid the per-sample volts→code conversion.

### Multi-channel

```go
// Interleaved samples: [ch0_s0, ch1_s0, ch0_s1, ch1_s1, ...]
h, err := dev.LoopRaw([]int{0, 1}, rate, interleavedSamples)
```

## AOutScanHandle

| Method | Description |
|--------|-------------|
| `Start()` | Begin scan (called automatically by PlayOnce / Loop helpers) |
| `Stop() error` | Abort scan; blocks until the hardware stops |
| `Wait(ctx) error` | Block until scan finishes (finite only) or ctx cancelled |
| `Err() error` | Return any error from the scan |
| `Channels() []int` | Configured DAC channel queue |
| `Rate() int` | Configured output rate |

## Underrun handling

If the host fails to refill the device FIFO in time, an underrun is detected
via the status register. The default policy is to tolerate the underrun,
restart the scan, and continue:

```go
h, err := dev.Loop(ch, rate, samples,
    device.WithUnderrunCallback(func(frames uint64, t time.Time) {
        log.Printf("underrun after %d frames", frames)
    }),
)
```

A safety valve caps recovery at `maxAOutUnderrunsPerSecond` (3) per second; if
exceeded the scan stops with `ErrAOutUnderrun`.

To fail immediately on the first underrun:

```go
h, err := dev.Loop(ch, rate, samples, device.WithStopOnUnderrun())
```

## Zero-on-close

By default `Device.Close()` writes 0 V to both DAC channels before releasing
the USB device. This prevents the DAC from holding its last output value after
the application exits.

To suppress the zero-out (e.g. to hold a DC offset across reconnections):

```go
dev, err := device.Open(device.KeepAOutOnClose())
```

## Calibration warm-up

DAC output accuracy improves after approximately 15 minutes of warm-up.
Calibration coefficients are read from EEPROM by `dev.Init()`.

## Known limits

- No DIO-in-AOut-scan support in Phase 1; the scan queue only accepts 0
  (DAC0) and 1 (DAC1).
- Device FIFO is 8 KiB (2048 two-channel scans). The host-side ring targets
  ~10 ms of data per URB.
- No FIFO fill-level query; underrun is detected via the status register
  (`AOutScanUnder` bit), which may have latch-until-read behaviour — verify
  on your hardware.

## Streaming with AOutWriter

`AOutWriter` provides a `Write`-based API for continuous output. The caller
pushes `[]uint16` chunks at its own pace; backpressure flows from the device
FIFO through the bulk-OUT ring back to the calling goroutine.

```go
cfg := device.AOutScanConfig{
    Channels: []int{0},
    Rate:     100_000,
    Count:    0, // continuous
}

w, err := dev.NewAOutWriter(cfg)
if err != nil { ... }
defer w.Close()

if err := w.Start(); err != nil { ... }

buf := make([]uint16, 2048)
for {
    n, err := readUint16LE(src, buf)  // e.g., from a file or synthesis
    if n > 0 {
        if _, werr := w.Write(buf[:n]); werr != nil { break }
    }
    if err != nil { break }
}
```

`Close` signals end-of-stream and blocks until the device FIFO drains. `Abort`
returns immediately and stops the scan asynchronously.

### AOutWriter API

| Method | Description |
|--------|-------------|
| `Start() error` | Arm the scan; begin consuming Write calls |
| `Write([]uint16) (int, error)` | Stream samples; blocks under backpressure |
| `Close() error` | Drain and stop; idempotent |
| `Abort() error` | Cancel immediately; returns without waiting |
| `Err() error` | Return any scan error |

### Backpressure

The ring has `DefaultAOutPipelineDepth` (32) URB slots, each targeting ~10 ms
of data. `Write` blocks until a slot is free, so steady-state write rate matches
device consumption. Callers that write faster than the device rate stall;
callers that write slower cause underruns (handled by the Phase 1 underrun
policy).

## See also

- [`docs/scanning-pipeline.md`](scanning-pipeline.md) — pipeline architecture
- [`docs/sync.md`](sync.md) — AI/AO synchronization guide
- [`_examples/analog_out/main.go`](../_examples/analog_out/main.go) — single-shot ramp
- [`_examples/play_waveform/main.go`](../_examples/play_waveform/main.go) — PlayOnce + Loop
- [`_examples/stream_aout/main.go`](../_examples/stream_aout/main.go) — AOutWriter from file
- [`_examples/synced_scan/main.go`](../_examples/synced_scan/main.go) — simultaneous AI + AO
