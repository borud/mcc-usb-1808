# Digital I/O

The USB-1808 has 4 digital I/O pins (bits 0–3), each individually configurable
as input or output.

## Pin Direction

The tristate register controls pin direction. A `1` bit means input, `0` means
output. The direction value is `uint16`.

```go
// Set all 4 pins as inputs.
err := dev.SetDigitalDirection(uint16(0x0F))

// Set pins 0–1 as inputs, 2–3 as outputs.
err := dev.SetDigitalDirection(uint16(0x03))

// Read current direction.
dir, err := dev.DigitalDirection() // dir is uint16
```

## Reading

Read the current state of all digital input pins:

```go
val, err := dev.ReadDigital()
// val is uint8, bits 0-3 are the pin states.
```

## Writing

Write to the output latch register (only affects pins configured as outputs):

```go
err := dev.WriteDigital(0x05)  // Set pins 0 and 2 high.
```

Read back the current latch value:

```go
latch, err := dev.DigitalLatch()
```

## Digital I/O in Scans

Digital I/O can be included in both input and output scan queues:

- Input scan: use `ScanChanDIO` (8) in the channel list.
- Output scan: use `AOutScanChanDIO` (2) in the channel list.

When read via the scan engine, the 4-bit digital value is returned as a 32-bit
word (input scan) or written as a 16-bit word (output scan).
