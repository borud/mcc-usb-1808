# Counters and Encoders

The USB-1808 has 4 counter channels: 2 event counters and 2 quadrature
encoders.

| Constant   | Index | Type               |
|------------|-------|--------------------|
| `Counter0` | 0     | Event counter      |
| `Counter1` | 1     | Event counter      |
| `Encoder0` | 2     | Quadrature encoder |
| `Encoder1` | 3     | Quadrature encoder |

## Reading and Writing

All counters are 32-bit:

```go
val, err := dev.ReadCounter(usb1808.Counter0)
err := dev.WriteCounter(usb1808.Counter0, 0)  // Reset to zero.
```

## Counter Modes (Counters 0–1)

Set the counting mode for event counters:

```go
err := dev.SetCounterMode(usb1808.Counter0, usb1808.CounterTotalize)
```

**Mode constants** (combine base mode with multiplier and tick size):

| Constant            | Value  | Description           |
|---------------------|--------|-----------------------|
| `CounterTotalize`   | `0x00` | Totalize events       |
| `CounterPeriod`     | `0x01` | Period measurement     |
| `CounterPulseWidth` | `0x02` | Pulse width measurement|
| `CounterTiming`     | `0x03` | Timing mode           |

**Period multipliers:**

| Constant          | Value  |
|-------------------|--------|
| `PeriodMode1X`    | `0x00` |
| `PeriodMode10X`   | `0x04` |
| `PeriodMode100X`  | `0x08` |
| `PeriodMode1000X` | `0x0C` |

**Tick sizes:**

| Constant          | Value  | Resolution  |
|-------------------|--------|-------------|
| `TickSize20NS`    | `0x00` | 20 ns       |
| `TickSize200NS`   | `0x10` | 200 ns      |
| `TickSize2000NS`  | `0x20` | 2 us        |
| `TickSize20000NS` | `0x30` | 20 us       |

Example with combined flags:

```go
mode := usb1808.CounterPeriod | usb1808.PeriodMode10X | usb1808.TickSize200NS
err := dev.SetCounterMode(usb1808.Counter0, mode)
```

## Counter Options (Counters 0–1)

```go
err := dev.SetCounterOptions(usb1808.Counter0, usb1808.CounterClearOnRead)
opts, err := dev.CounterOptions(usb1808.Counter0)
```

| Constant             | Value  | Description          |
|----------------------|--------|----------------------|
| `CounterClearOnRead` | `0x01` | Clear value on read  |
| `CounterNoRecycle`   | `0x02` | Stop at limit        |
| `CounterCountDown`   | `0x04` | Count downward       |
| `CounterRangeLimit`  | `0x08` | Enable min/max limits|
| `CounterFallingEdge` | `0x10` | Count on falling edge|

## Counter Limits

Set minimum and maximum limits (requires `CounterRangeLimit` option):

```go
err := dev.SetCounterLimits(usb1808.Counter0, 0, 0)         // Min = 0
err := dev.SetCounterLimits(usb1808.Counter0, 1, 1000000)   // Max = 1000000

val, err := dev.CounterLimits(usb1808.Counter0, 0)  // Read min
val, err := dev.CounterLimits(usb1808.Counter0, 1)  // Read max
```

The `index` parameter: 0 = minimum, 1 = maximum.

## Encoder Options (Encoders 2–3)

Quadrature encoders use a different set of option flags:

```go
err := dev.SetCounterOptions(usb1808.Encoder0, usb1808.EncoderX4)
```

| Constant           | Value  | Description         |
|--------------------|--------|---------------------|
| `EncoderX1`        | `0x00` | X1 quadrature       |
| `EncoderX2`        | `0x01` | X2 quadrature       |
| `EncoderX4`        | `0x02` | X4 quadrature       |
| `EncoderClearOnZ`  | `0x04` | Clear on Z pulse    |
| `EncoderLatchOnZ`  | `0x08` | Latch on Z pulse    |
| `EncoderNoRecycle` | `0x10` | Stop at limit       |
| `EncoderRangeLimit`| `0x20` | Enable range limits |

## Counter Parameters

Read or write mode and options as a combined 2-byte value:

```go
params, err := dev.CounterParams(usb1808.Counter0)  // []byte{mode, options}
err := dev.SetCounterParams(usb1808.Counter0, []byte{mode, options})
```

## Counters in Scans

Counters and encoders can be included in the analog input scan queue:

| Constant           | Value | Source     |
|--------------------|-------|------------|
| `ScanChanCounter0` | 9     | Counter 0  |
| `ScanChanCounter1` | 10    | Counter 1  |
| `ScanChanEncoder0` | 11    | Encoder 0  |
| `ScanChanEncoder1` | 12    | Encoder 1  |

Values are returned as raw 32-bit integers (no voltage conversion).
