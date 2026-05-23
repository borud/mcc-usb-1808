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

## See also

- [`docs/scanning-pipeline.md`](scanning-pipeline.md) — pipeline architecture
- [`_examples/analog_out/main.go`](../_examples/analog_out/main.go) — single-shot ramp
- [`_examples/play_waveform/main.go`](../_examples/play_waveform/main.go) — PlayOnce + Loop
