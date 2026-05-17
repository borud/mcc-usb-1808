# Analog Input

The USB-1808 has 8 analog input channels with an 18-bit simultaneous-sampling
ADC.

## Ranges and Modes

Each channel can be configured with a voltage range and input mode.

**Ranges:**

| Constant | Range     | Type     |
|----------|-----------|----------|
| `BP10V`  | +/-10V    | Bipolar  |
| `BP5V`   | +/-5V     | Bipolar  |
| `UP10V`  | 0-10V     | Unipolar |
| `UP5V`   | 0-5V      | Unipolar |

**Input modes:**

| Constant       | Description    |
|----------------|----------------|
| `Differential` | Differential   |
| `SingleEnded`  | Single-ended   |
| `Grounded`     | Grounded input |

Mode code 2 is undefined by the hardware and is rejected with
`ErrInvalidMode`.

## Continuous Scanning

For high-speed continuous acquisition, use the scan engine. The FPGA maintains
a scan queue of up to 13 elements that can mix analog input channels, digital
I/O, counters, and encoders.

### Scan Queue Channels

| Constant           | Value | Source              |
|--------------------|-------|---------------------|
| `ScanChanAIn0`-`7` | 0-7   | Analog input 0-7    |
| `ScanChanDIO`      | 8     | Digital I/O         |
| `ScanChanCounter0` | 9     | Event counter 0     |
| `ScanChanCounter1` | 10    | Event counter 1     |
| `ScanChanEncoder0` | 11    | Quadrature encoder 0|
| `ScanChanEncoder1` | 12    | Quadrature encoder 1|

### Using CreateScan

`CreateScan` configures the hardware and returns a `*ScanHandle`. Call `Start`
to begin acquisition, read from `Chunks()`, and `Stop` to end.

```go
cfg := device.ScanConfig{
    Channels: []device.ChannelConfig{
        {Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
        {Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
        {Index: 8, Type: device.ChannelTypeDIO},
    },
    Rate:  10000, // 10 kHz per channel
    Count: 1000,  // 1000 scans (0 = continuous)
}

h, err := dev.CreateScan(cfg)
if err != nil {
    log.Fatal(err)
}
if err := h.Start(); err != nil {
    log.Fatal(err)
}
for chunk := range h.Chunks() {
    // chunk is []byte, packed little-endian uint32s
    // len(chunk) is a multiple of frameSize (nChannels * 4)
}
h.Stop()
```

### ScanConfig

| Field      | Type             | Description                              |
|------------|------------------|------------------------------------------|
| `Channels` | `[]ChannelConfig`| Scan queue channel configurations.       |
| `Rate`     | `int`            | Sample rate in Hz per channel.           |
| `Count`    | `uint32`         | Total scans. 0 = continuous.             |
| `Options`  | `uint8`          | Scan option flags (see below).           |

### ChannelConfig

| Field   | Type          | Description                       |
|---------|---------------|-----------------------------------|
| `Index` | `int`         | Queue channel selector (0-12).    |
| `Type`  | `ChannelType` | Analog, DIO, Counter, or Encoder. |
| `Range` | `Range`       | Voltage range (analog only).      |
| `Mode`  | `InputMode`   | Input mode (analog only).         |

**Scan option flags:**

| Constant                  | Value  | Description                    |
|---------------------------|--------|--------------------------------|
| `ScanOptExternalTrigger`  | `0x01` | Use external trigger.          |
| `ScanOptPatternDetection` | `0x02` | Use pattern detection trigger. |
| `ScanOptRetriggerMode`    | `0x04` | Retrigger mode.                |
| `ScanOptCounterValue`     | `0x08` | Maintain counter on scan start.|
| `ScanOptSingleIO`         | `0x10` | Single-sample transfer mode.   |

### ScanOption Functions

| Option                   | Default | Description                           |
|--------------------------|---------|---------------------------------------|
| `WithPipelineDepth(n)`   | 32      | Read-ahead batches buffered.          |
| `WithConcurrentReaders(n)` | 4    | Goroutines issuing USB bulk reads.    |

### Decoding Scan Data

Use the `codec` package to decode raw chunks into typed values:

```go
import "github.com/borud/mcc-usb-1808/v4/codec"

dec := codec.NewDecoder(cfg.Channels, dev.CalibrationTable())
for chunk := range h.Chunks() {
    for frame := range dec.Frames(chunk) {
        voltage := frame.Voltage(0)  // calibrated voltage for channel 0
        dio := frame.Digital()       // digital I/O byte
    }
}
```

### Voltage Conversion

`Calibration.ToVolts` converts a raw 18-bit ADC value to voltage using
factory calibration coefficients:

```go
cal := dev.CalibrationTable()
v := cal[channel][rangeCode].ToVolts(rawValue, rangeCode)
```

The conversion applies the factory calibration slope and offset, then maps the
result to the voltage range:

- Bipolar +/-10V: `(cal - 131072) * 10 / 131072`
- Bipolar +/-5V: `(cal - 131072) * 5 / 131072`
- Unipolar 0-10V: `cal * 10 / 262143`
- Unipolar 0-5V: `cal * 5 / 262143`

Unipolar values are clamped to `[0, 262143]` before conversion.

### Overrun Handling

If the host does not read data fast enough, the device FIFO overruns.
The scan handle detects this via the status register and reports
`ErrScanOverrun` via `ScanHandle.Err()` after stopping the scan and clearing
the FIFO.

See [High-Rate Capture Troubleshooting](high-rate-capture.md) for tuning
guidance when overruns occur at high sample rates.
