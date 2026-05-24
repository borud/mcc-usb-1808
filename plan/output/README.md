# Analog Output (AOut) — Implementation Plan

This directory holds the implementation plan for adding analog output to the
USB-1808/1808X Go driver. It is split into two phases:

- [phase1.md](phase1.md) — single-shot writes, paced scan (PlayOnce / Loop),
  URB-ring + refill engine, underrun policy, tests, examples, docs.
- [phase2.md](phase2.md) — `AOutWriter` streaming type, sync example, more docs.

Phase 1 is fully self-contained and shippable. Phase 2 layers on top of the
Phase 1 engine.

## Required skills (consult before writing code)

Before writing or modifying code in any step below, read and follow the
relevant skill files in `~/.claude/skills/`:

- **`golang-code-style`** — all Go code in `device/`, `transport/`, `wire/`,
  `_examples/`, `cmd/`. Style, control flow, naming, comment policy,
  proto↔model conventions.
- **`golang-logging`** — any `log/slog` use in the AOut handle, writer, or
  CLI. Loggers passed explicitly in libraries; CLIs expose
  `--log-level`/`--log-format`.
- **`golang-cli`** — required when adding the `daq aout` subcommand
  (Phase 1 step 14). All CLIs use `alecthomas/kong`.
- **`makefiles`** — required for any Makefile changes (Phase 1 step 16 if
  new targets like `make integration` are added).

`golang-db`, `protobuffer`, and `elm` are not in scope for AOut. If scope
expands to persistence (e.g. capture format extensions for AOut events),
add `golang-db` to the required-skills list.

## Locked design (output of the grill-me session)

### Public API — Phase 1

```go
// Single-shot (refuses with ErrAOutScanRunning while a scan is active):
func (d *Device) AnalogOut(channel int, volts float64) error
func (d *Device) AnalogOutRaw(channel int, value uint16) error
func (d *Device) VoltsToAOutRaw(channel int, volts float64) uint16

// Paced scan:
type AOutScanConfig struct {
    Channels []int   // queue, each value 0 or 1 (DIO slot hidden in Phase 1)
    Rate     int     // Hz per channel
    Count    uint32  // 0 = loop forever, >0 = play once then stop
    Options  uint8   // ScanOptExternalTrigger | ScanOptPatternDetection | ScanOptRetriggerMode
}

func (d *Device) CreateAOutScan(cfg AOutScanConfig, opts ...AOutScanOption) (*AOutScanHandle, error)
func (d *Device) PlayOnce(channels []int, rate int, samples []float64) (*AOutScanHandle, error)
func (d *Device) PlayOnceRaw(channels []int, rate int, samples []uint16) (*AOutScanHandle, error)
func (d *Device) Loop(channels []int, rate int, samples []float64) (*AOutScanHandle, error)
func (d *Device) LoopRaw(channels []int, rate int, samples []uint16) (*AOutScanHandle, error)

type AOutScanHandle struct { /* parallel to ScanHandle, no shared type */ }

func (h *AOutScanHandle) Start() error
func (h *AOutScanHandle) Stop() error                  // abort
func (h *AOutScanHandle) Wait(ctx context.Context) error // drain (finite scans)
func (h *AOutScanHandle) Err() error
func (h *AOutScanHandle) Channels() []int
func (h *AOutScanHandle) Rate() int
```

### Options

```go
func WithAOutPipelineDepth(n int) AOutScanOption
func WithUnderrunCallback(fn UnderrunFunc) AOutScanOption
func WithStopOnUnderrun() AOutScanOption
```

### Public API — Phase 2

```go
func (d *Device) NewAOutWriter(cfg AOutScanConfig, opts ...AOutScanOption) (*AOutWriter, error)

func (w *AOutWriter) Start() error
func (w *AOutWriter) Write(samples []uint16) (int, error)
func (w *AOutWriter) Close() error  // drain
func (w *AOutWriter) Abort() error
func (w *AOutWriter) Err() error
```

### Behavior

| Aspect | Decision |
|---|---|
| Playback shapes | Phase 1: single-shot + PlayOnce + Loop. Phase 2: streaming Writer |
| Sample-provision (streaming) | `Writer.Write([]uint16)`, synchronous, blocks for backpressure |
| Types | Volts on convenience methods; raw `uint16` on streaming and `*Raw` counterparts |
| DIO slot | Hidden in Phase 1; additive extension path reserved |
| AI/AO sync | Primitives only (`SetTriggerConfig`, option bits), plus example + doc |
| Handle structure | Parallel `AOutScanHandle`; existing `ScanHandle` (AIn) not renamed |
| Package layout | Flat under `device/`, prefix `scan_out_*.go` |
| Underrun policy | Tolerate + callback + auto-restart; safety valve 3/sec → `ErrAOutUnderrun`; `WithStopOnUnderrun()` opt-out |
| Single-shot ↔ scan | Refuse with `ErrAOutScanRunning`; no implicit stop |
| `Device.Close()` | Writes 0 V to both AOut channels by default; `KeepAOutOnClose()` opens-time option to disable |
| Stop semantics | `Stop()` aborts; `Wait()` drains (finite only) |
| Warm-up warning | Documentation only |

### Wire protocol constants (already verified in existing code)

| Item | Value |
|---|---|
| AOut single-shot | `cmdAOut = 0x18`, `wValue=DAC code`, `wIndex=channel(0|1)` |
| AOut scan config | `cmdAOutScanConf = 0x19`, `wIndex=lastChan-1`, 3 bytes data |
| AOut scan start | `cmdAOutScanStart = 0x1A`, 13-byte payload |
| AOut scan stop | `cmdAOutScanStop = 0x1B`, no data |
| AOut clear FIFO | `cmdAOutClearFIFO = 0x1C`, no data |
| Bulk OUT endpoint | `0x02` (EP2 OUT) |
| Sample format | `uint16` LE, offset-binary, interleaved by queue order |
| DAC code zero | `0x8000` = 0 V; `0x0000` = −10 V; `0xFFFF` = +10 V |
| Scan-start payload | `count u32 | retrig u32 | pacer u32 | opts u8` (13 bytes) |
| Options bits | bit 0 = ext trigger, bit 1 = pattern detect, bit 2 = retrigger |
| Scan queue values | `0` = DAC0, `1` = DAC1, `2` = DIO (hidden) |
| Hardware FIFO | 8 KiB (= 2048 two-channel scans) |
| Max rate / channel | 125 kS/s (1808) / 500 kS/s (1808X) |
| Voltage range | fixed ±10 V |
| Calibration EEPROM | DAC0 slope/offset at `0x7100`/`0x7104`, DAC1 at `0x7108`/`0x710C`; float32 LE |
| ZLP | bulk write whose length is a multiple of 512 needs a trailing ZLP |
| Mutex | single-shot AOut stalls during scan; ScanStart stalls during scan |

### Sources for the protocol details

- `wjasper/Linux_Drivers/USB/mcc-libusb/usb-1808.{c,h}` and `test-usb1808.c`
- `mccdaq/uldaq/src/usb/ao/AoUsb1808.{cpp,h}`,
  `mccdaq/uldaq/src/usb/daqo/DaqOUsb1808.{cpp,h}`,
  `mccdaq/uldaq/src/usb/Usb1808.{cpp,h}`,
  `mccdaq/uldaq/src/usb/UsbScanTransferOut.{cpp,h}`,
  `mccdaq/uldaq/src/usb/daqo/DaqOUsbBase.cpp`
