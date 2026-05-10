# Analog Input

The USB-1808 has 8 analog input channels with an 18-bit simultaneous-sampling
ADC.

## Ranges and Modes

Each channel can be configured with a voltage range and input mode.

**Ranges:**

| Constant | Range     | Type     |
|----------|-----------|----------|
| `BP10V`  | ±10V      | Bipolar  |
| `BP5V`   | ±5V       | Bipolar  |
| `UP10V`  | 0–10V     | Unipolar |
| `UP5V`   | 0–5V      | Unipolar |

**Input modes:**

| Constant       | Description    |
|----------------|----------------|
| `Differential` | Differential   |
| `SingleEnded`  | Single-ended   |
| `Grounded`     | Grounded input |

Mode code 2 is undefined by the hardware and is rejected with
`ErrInvalidMode`.

## Configuration

Configure all 8 channels before reading. Each channel gets a range and mode:

```go
configs := make([]usb1808.AnalogInChannelConfig, usb1808.NumAInChannels)
for i := range configs {
    configs[i] = usb1808.AnalogInChannelConfig{
        Channel: i,
        Range:   usb1808.BP10V,
        Mode:    usb1808.Differential,
    }
}
if err := dev.ConfigureAnalogIn(configs); err != nil {
    log.Fatal(err)
}
```

To read back the current configuration:

```go
configs, err := dev.AnalogInConfig()
```

## Single Read

Read all 8 channels as calibrated voltages:

```go
volts, err := dev.AnalogIn()
// volts is [8]float64
```

Or as raw 18-bit ADC values:

```go
raw, err := dev.AnalogInRaw()
// raw is [8]uint32
```

## Voltage Conversion

`AnalogInToVolts` converts a raw 18-bit value to voltage using the device's
calibration coefficients:

```go
v := dev.AnalogInToVolts(rawValue, channel, usb1808.BP10V)
```

The conversion applies the factory calibration slope and offset, then maps the
result to the voltage range:

- Bipolar ±10V: `(cal - 131072) * 10 / 131072`
- Bipolar ±5V: `(cal - 131072) * 5 / 131072`
- Unipolar 0–10V: `cal * 10 / 262143`
- Unipolar 0–5V: `cal * 5 / 262143`

Unipolar values are clamped to `[0, 262143]` before conversion.

## Continuous Scanning

For high-speed continuous acquisition, use the scan engine. The FPGA maintains
a scan queue of up to 13 elements that can mix analog input channels, digital
I/O, counters, and encoders.

### Scan Queue Channels

| Constant           | Value | Source              |
|--------------------|-------|---------------------|
| `ScanChanAIn0`–`7` | 0–7   | Analog input 0–7    |
| `ScanChanDIO`      | 8     | Digital I/O         |
| `ScanChanCounter0` | 9     | Event counter 0     |
| `ScanChanCounter1` | 10    | Event counter 1     |
| `ScanChanEncoder0` | 11    | Quadrature encoder 0|
| `ScanChanEncoder1` | 12    | Quadrature encoder 1|

### Using ScanAnalogIn

`ScanAnalogIn` is a pull-based iterator that configures the queue, starts the
scan, reads data, converts to calibrated voltages, and stops the scan on
completion. Each iteration yields one scan frame (one value per channel).

```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
defer cancel()

cfg := usb1808.AnalogInScanConfig{
    Channels: []int{0, 1, 2, 3},  // Channels to sample.
    Rate:     10000.0,             // 10 kHz per channel.
    Count:    1000,                // 1000 scans (0 = continuous).
}

for frame, err := range dev.ScanAnalogIn(ctx, cfg) {
    if err != nil {
        log.Fatal(err)
    }
    // frame is []float64 with one value per channel.
    fmt.Println(frame)
}
```

Non-analog channels (DIO, counters, encoders) are returned as raw `float64`
values without voltage conversion.

### AnalogInScanConfig

| Field           | Type      | Description                              |
|-----------------|-----------|------------------------------------------|
| `Channels`      | `[]int`   | Scan queue channel selectors (0–12).     |
| `Rate`          | `float64` | Sample rate in Hz per channel.           |
| `Count`         | `uint32`  | Total scans. 0 = continuous.             |
| `RetrigCount`   | `uint32`  | Scans per retrigger. 0 = no retrigger.   |
| `Options`       | `uint8`   | Scan option flags (see below).           |
| `PacketSize`    | `uint8`   | Samples-1 per USB packet (0xFF = max).   |
| `PipelineDepth` | `int`     | Read-ahead batches buffered (default 32).|

**Scan option flags:**

| Constant                  | Value  | Description                    |
|---------------------------|--------|--------------------------------|
| `ScanOptExternalTrigger`  | `0x01` | Use external trigger.          |
| `ScanOptPatternDetection` | `0x02` | Use pattern detection trigger. |
| `ScanOptRetriggerMode`    | `0x04` | Retrigger mode.                |
| `ScanOptCounterValue`     | `0x08` | Maintain counter on scan start.|
| `ScanOptSingleIO`         | `0x10` | Single-sample transfer mode.   |

### Low-Level Scan API

For more control, use the individual scan methods:

```go
// Configure scan queue.
dev.ConfigureAnalogInScan([]int{0, 1, 2, 3})

// Start the scan.
dev.StartAnalogInScan(cfg)

// Read calibrated voltages (nScans at a time).
volts, err := dev.ReadAnalogInScan(ctx, 100)

// Or read raw uint32 values.
raw, err := dev.ReadAnalogInScanRaw(ctx, nChannels, nScans, timeout)

// Stop the scan.
dev.StopAnalogInScan()
```

### High-Throughput Scan APIs

For sustained high-rate capture, use `ScanAnalogInRaw` or `ScanAnalogInBulk`
instead of `ScanAnalogIn`. These avoid per-sample calibration overhead and use
a multi-reader USB pipeline.

`ScanAnalogInRaw` yields raw `[]uint32` frames without voltage conversion:

```go
for frame, err := range dev.ScanAnalogInRaw(ctx, cfg) {
    // frame is []uint32
}
```

`ScanAnalogInBulk` yields raw `[]byte` slices directly from USB bulk
transfers, with no unpacking. This is the path used by `daq capture`:

```go
for data, err := range dev.ScanAnalogInBulk(ctx, cfg) {
    // data is []byte, packed little-endian uint32s
}
```

Both APIs use a configurable read-ahead pipeline (`PipelineDepth`, default 32
batches) with multiple concurrent USB readers to keep the device FIFO drained.

### Overrun Handling

If the host does not read data fast enough, the device FIFO overruns.
`ReadAnalogInScanRaw` detects this via the status register and returns
`ErrScanOverrun` after stopping the scan and clearing the FIFO.

See [High-Rate Capture Troubleshooting](high-rate-capture.md) for tuning
guidance when overruns occur at high sample rates.
