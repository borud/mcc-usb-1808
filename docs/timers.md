# Timers

The USB-1808 has 2 pulse output timers driven by a 100 MHz base clock.

| Constant | Index |
|----------|-------|
| `Timer0` | 0     |
| `Timer1` | 1     |

## Starting and Stopping

```go
cfg := usb1808.TimerConfig{
    Frequency: 1000.0,  // 1 kHz output
    DutyCycle: 0.5,     // 50%
    Count:     0,       // 0 = continuous
    Delay:     0.0,     // No initial delay
}
err := dev.StartTimer(usb1808.Timer0, cfg)

// ...

err := dev.StopTimer(usb1808.Timer0)
```

`StartTimer` sets the timer parameters and enables it. `StopTimer` disables it.

## TimerConfig

| Field       | Type      | Description                        |
|-------------|-----------|------------------------------------|
| `Frequency` | `float64` | Output frequency in Hz.            |
| `DutyCycle` | `float64` | Duty cycle, 0.0 to 1.0.           |
| `Count`     | `uint32`  | Number of pulses. 0 = continuous.  |
| `Delay`     | `float64` | Initial delay in seconds.          |

Internally, frequency and duty cycle are converted to period and pulse width
using the 100 MHz base clock:

- Period = `BaseClock / frequency - 1`
- Pulse width = `(period + 1) * dutyCycle - 1`

## Reading Parameters

The firmware returns incorrect values when timer parameters are read back, so
the library caches the last written values:

```go
cfg, err := dev.TimerParams(usb1808.Timer0)
```

## Timer Control Register

For direct control register access:

```go
ctrl, err := dev.TimerControl(usb1808.Timer0)
err := dev.SetTimerControl(usb1808.Timer0, usb1808.TimerEnable|usb1808.TimerInverted)
```

**Control bits:**

| Constant          | Value  | Description                 |
|-------------------|--------|-----------------------------|
| `TimerEnable`     | `0x01` | Enable the timer.           |
| `TimerRunning`    | `0x02` | Timer is running (read-only).|
| `TimerInverted`   | `0x04` | Inverted output polarity.   |
| `TimerOTrigBegin` | `0x10` | Begin on OTRIG.             |
| `TimerOTrig`      | `0x40` | Continue on OTRIG.          |
