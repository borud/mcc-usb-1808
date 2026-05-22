# USB-1808 Go Driver Manual

Go driver for the Measurement Computing USB-1808 and USB-1808X multifunction
data acquisition (DAQ) devices.

The USB-1808 and USB-1808X are identical except for maximum scan rates
(per channel, simultaneous across all active channels):

| Feature          | USB-1808          | USB-1808X         |
|------------------|-------------------|-------------------|
| Analog input     | 50 kS/s per ch    | 200 kS/s per ch   |
| Analog output    | 125 kS/s          | 500 kS/s          |

At maximum rate, 8 channels at 200 kS/s with raw uint32 samples produces
8 x 200,000 x 4 = 6.4 MB/s of data before file or container overhead.

Both models provide:

- 8 analog input channels (18-bit simultaneous ADC)
- 2 analog output channels (16-bit DAC, fixed +/-10V)
- 4 digital I/O pins (individually configurable)
- 2 event counters + 2 quadrature encoders
- 2 pulse output timers (100 MHz base clock)
- FPGA-based scan engine for continuous streaming

## Documentation

- [Getting Started](getting-started.md) -- prerequisites, installation, quick start
- [Analog Input](analog-input.md) -- scanning, decoding, and voltage conversion
- [Calibration](calibration.md) -- factory calibration and device info
- [Capture](capture.md) -- binary capture format, reading, writing, and export
- [High-Rate Capture](high-rate-capture.md) -- FIFO overrun troubleshooting and tuning
- [CLI Tool](cli.md) -- the `daq` command-line tool
- [Errors](errors.md) -- sentinel errors reference

## Package Structure

```
github.com/borud/mcc-usb-1808/v4
├── device      Core driver: open, init, configure, scan
├── codec       Decode raw scan chunks into frames and voltages
├── capture     Binary capture format: segmented reader/writer
├── capture/export  Export captures to CSV, SQLite, Parquet, WAV
├── stream      Fan-out: broadcast scan data to multiple consumers
├── wire        Protocol-level serialization (internal)
└── transport   USB abstraction layer (internal)
```

Most applications use three packages:

- **`device`** -- open the hardware, configure a scan, and read raw data via
  `CreateScan` / `ScanHandle`.
- **`codec`** -- decode raw scan chunks into typed `Frame` values with
  calibrated voltages, digital I/O, counter, and encoder readings.
- **`capture`** -- record raw scan data to disk and replay it later. The writer
  accepts raw bytes directly from `ScanHandle.Chunks()` for a zero-copy fast
  path. The reader produces `Frame` values with calibrated voltages, just like
  `codec`.

For multi-consumer pipelines (e.g. simultaneous recording and live display),
the **`stream`** package provides a `Fanout` that copies each chunk to multiple
subscribers without blocking the scan.

The **`wire`** and **`transport`** packages are internal to the driver and not
intended for direct use.

## Import

```go
import "github.com/borud/mcc-usb-1808/v4/device"
```
