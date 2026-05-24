# AI/AO Synchronization

This guide explains how to run analog input (AIn) and analog output (AOut) scans
simultaneously on the USB-1808/1808X.

## Overview

The USB-1808X has independent AIn and AOut scan engines, each with its own
pacer and FIFO. Synchronization means:

1. Both scans start on the same trigger edge (temporal alignment).
2. Both pacers run from the same clock source (frequency alignment, no drift).

The USB-1808 supports software triggering only; external trigger requires
the USB-1808X.

## Software trigger (portable, non-deterministic latency)

Start AOut first (so its FIFO is preloaded), then start AIn. The two scans
run independently:

```go
// AOut: preload FIFO with one period of a sine wave.
aout, err := dev.Loop([]int{0}, rate, sineTable)
if err != nil { ... }
defer aout.Stop()

// AIn: capture response.
scan, err := dev.CreateScan(ainCfg)
if err != nil { ... }
scan.Start()
```

Latency between AOut start and AIn start is O(microseconds) to O(milliseconds)
depending on OS scheduling. This is adequate for stimulus-response experiments
where exact phase alignment is not required.

## External trigger (USB-1808X only)

Both scans arm themselves and wait for a common trigger edge before their
pacers begin. This eliminates the scheduling jitter and reduces phase skew to
the device's clock resolution (~8 ns at 125 MHz base clock).

### Hardware setup

Connect a TTL trigger source to **TRIG_IN** (pin 37 on the screw terminal
block). The same signal can be looped back to the AI and AO trigger inputs via
a Y-splitter cable.

### Configuration

```go
// Configure the trigger: rising-edge sensitive.
if err := dev.SetTriggerConfig(device.TriggerEdge | device.TriggerHigh); err != nil {
    log.Fatal(err)
}

// AOut scan with external trigger bit set.
aoutCfg := device.AOutScanConfig{
    Channels: []int{0},
    Rate:     rate,
    Count:    0,
    Options:  device.ScanOptExternalTrigger,
}
aout, err := dev.Loop(aoutCfg.Channels, aoutCfg.Rate, sineTable,
    /* opts ... */)

// AIn scan with external trigger bit set.
ainCfg := device.ScanConfig{
    Channels: ainChannels,
    Rate:     rate,
    Count:    uint32(rate * seconds),
    Options:  device.ScanOptExternalTrigger,
}
scan, err := dev.CreateScan(ainCfg)
scan.Start()

// Both scans are now armed. Apply the external trigger pulse.
// The pulse causes both AIn and AOut to begin simultaneously.
```

### Recommended start order

1. Configure AOut scan + preload FIFO (`dev.Loop` or `dev.NewAOutWriter` + `Start`).
2. Start AIn scan (`scan.Start`).
3. Assert the external trigger pulse.

Starting AOut first ensures the FIFO has data before the trigger fires, avoiding
an immediate underrun.

## Shared pacer clock (hardware clock synchronization)

For measurements requiring sub-microsecond phase alignment between AIn and AOut,
you can route the AOut pacer output to the AIn pacer input via a jumper wire on
the screw terminal block:

| Signal | Pin |
|--------|-----|
| OCLKO  | Output Pacer Clock Out — AOut pacer output |
| ICLKI  | Input Pacer Clock In — AIn pacer input |

When AIn is configured with an external pacer (`pacer_period = 0` in the start
command), it clocks from whatever signal is on ICLKI. Connecting OCLKO → ICLKI
ensures both engines advance on exactly the same clock edge.

> **Note:** this hardware feature is not yet exposed as a first-class option in
> this driver. To use it, set `cfg.Options` to the appropriate external-pacer
> flag and verify the `pacer_period` field is sent as zero in the wire-level
> command. Contributions welcome.

## Caveats

- **Shared trigger register.** `SetTriggerConfig` applies to both AIn and AOut.
  You cannot configure one scan to use a rising-edge trigger and the other to
  use a falling-edge trigger.
- **FIFO preload.** The AOut device FIFO is 8 KiB. Preload at least one URB of
  data before the trigger fires to avoid an immediate underrun.
- **Software trigger latency.** OS scheduling variability means the two `Start`
  calls are never perfectly simultaneous. For sub-millisecond alignment, use
  the external trigger.
- **DAC holds last value.** The DAC output holds its last code after the scan
  ends. Call `dev.AnalogOut(ch, 0)` or use the default `KeepAOutOnClose=false`
  behavior (zeroes DAC on device close).

## See also

- [`docs/aout.md`](aout.md) — AOut API reference
- [`_examples/synced_scan/main.go`](../_examples/synced_scan/main.go) — software-trigger example
- [`_examples/stream_aout/main.go`](../_examples/stream_aout/main.go) — AOutWriter streaming
