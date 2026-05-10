# USB-1808 Go Driver Manual

Go driver for the Measurement Computing USB-1808 and USB-1808X multifunction
data acquisition (DAQ) devices.

The USB-1808 and USB-1808X are identical except for maximum scan rates:

| Feature          | USB-1808   | USB-1808X  |
|------------------|------------|------------|
| Analog input     | 50 kS/s    | 200 kS/s   |
| Analog output    | 125 kS/s   | 500 kS/s   |

Both models provide:

- 8 analog input channels (18-bit simultaneous ADC)
- 2 analog output channels (16-bit DAC, fixed ±10V)
- 4 digital I/O pins (individually configurable)
- 2 event counters + 2 quadrature encoders
- 2 pulse output timers (100 MHz base clock)
- FPGA-based scan engine for continuous streaming

## Documentation

- [Getting Started](getting-started.md) — prerequisites, installation, quick start
- [Analog Input](analog-input.md) — single reads and continuous scanning
- [Analog Output](analog-output.md) — single writes and output scanning
- [Digital I/O](digital-io.md) — pin direction, reading, and writing
- [Counters and Encoders](counters-encoders.md) — event counting and quadrature decoding
- [Timers](timers.md) — pulse output generation
- [Triggers](triggers.md) — external triggers and pattern detection
- [Calibration](calibration.md) — factory calibration and device info
- [Capture](capture.md) — binary capture format, reading, writing, and export
- [CLI Tool](cli.md) — the `daq` command-line tool
- [Errors](errors.md) — sentinel errors reference

## Import

```go
import "github.com/borud/mcc-usb-1808"
```
