# Analog Output

The USB-1808 has 2 analog output channels with 16-bit DACs. The output range
is fixed at ±10V.

## Single Write

Write a voltage to a channel:

```go
err := dev.AnalogOut(0, 5.0)   // Channel 0, +5.0 V
err := dev.AnalogOut(1, -2.5)  // Channel 1, -2.5 V
```

Or write a raw 16-bit DAC value (0–65535, where 32768 = 0V):

```go
err := dev.AnalogOutRaw(0, 40000)
```

`AnalogOut` checks the device status before writing and returns
`ErrAOutScanRunning` if an output scan is in progress.

## Voltage Conversion

`VoltsToAnalogOut` converts a voltage to a calibrated 16-bit DAC value:

```go
dac := dev.VoltsToAnalogOut(5.0, 0)  // channel 0
```

The formula is: `raw = voltage / 10.0 * 32768.0 + 32768.0`, then factory
calibration slope and offset are applied. The result is clamped to `[0, 65535]`.

## Output Scanning

For continuous waveform output, use the analog output scan engine.

### Output Scan Queue

| Constant            | Value | Destination      |
|---------------------|-------|------------------|
| `AOutScanChanAOut0` | 0     | Analog output 0  |
| `AOutScanChanAOut1` | 1     | Analog output 1  |
| `AOutScanChanDIO`   | 2     | Digital I/O      |

The queue holds up to 3 elements.

### Usage

```go
// Configure queue.
err := dev.ConfigureAnalogOutScan([]int{0, 1})

// Start scan.
cfg := usb1808.AnalogOutScanConfig{
    Channels: []int{0, 1},
    Rate:     50000.0,    // 50 kHz
    Count:    1000,       // 1000 scans (0 = continuous)
}
err := dev.StartAnalogOutScan(cfg)

// Write data as 16-bit LE samples, interleaved by queue position.
data := make([]byte, 2*1000*2)  // 2 channels * 1000 scans * 2 bytes
// ... populate with calibrated DAC values ...
n, err := dev.WriteAnalogOutScan(data)

// Stop when done.
err := dev.StopAnalogOutScan()
```

### AnalogOutScanConfig

| Field         | Type      | Description                          |
|---------------|-----------|--------------------------------------|
| `Channels`    | `[]int`   | Queue channel selectors (0–2).       |
| `Rate`        | `float64` | Sample rate in Hz.                   |
| `Count`       | `uint32`  | Total scans. 0 = continuous.         |
| `RetrigCount` | `uint32`  | Scans per retrigger.                 |
| `Options`     | `uint8`   | Option flags.                        |

**Option flags:**

| Constant          | Value  | Description      |
|-------------------|--------|------------------|
| `AOutOptTrigger`  | `0x10` | External trigger.|
| `AOutOptRetrigger`| `0x20` | Retrigger mode.  |

### Underrun

If the host does not write data fast enough, the device FIFO underruns. The
status bit `StatusAOutScanUnder` is set and can be checked via
`Status().AOutScanUnderrun()`.
