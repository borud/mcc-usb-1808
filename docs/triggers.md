# Triggers

The USB-1808 supports external trigger input and pattern detection for
starting or gating scans.

## External Trigger

Configure the external trigger input:

```go
err := dev.SetTriggerConfig(usb1808.TriggerEdge | usb1808.TriggerHigh)
cfg, err := dev.TriggerConfig()
```

**Trigger bits:**

| Constant      | Value  | Description                         |
|---------------|--------|-------------------------------------|
| `TriggerEdge` | `0x01` | 0 = level trigger, 1 = edge trigger |
| `TriggerHigh` | `0x02` | 0 = low/falling, 1 = high/rising    |

Combinations:

| TriggerEdge | TriggerHigh | Behavior            |
|-------------|-------------|---------------------|
| 0           | 0           | Active low level    |
| 0           | 1           | Active high level   |
| 1           | 0           | Falling edge        |
| 1           | 1           | Rising edge         |

## Pattern Detection

Pattern detection triggers based on the state of the digital I/O pins:

```go
cfg := usb1808.PatternDetectConfig{
    Value:   0x05,                       // Pattern to match.
    Mask:    0x0F,                       // Bits to compare.
    Options: usb1808.PatternEqual,       // Comparison mode.
}
err := dev.SetPatternDetect(cfg)

// Read back.
cfg, err := dev.PatternDetect()
```

**Comparison modes:**

| Constant            | Value  | Description                   |
|---------------------|--------|-------------------------------|
| `PatternEqual`      | `0x00` | Match when pins == value      |
| `PatternNotEqual`   | `0x02` | Match when pins != value      |
| `PatternGreaterThn` | `0x04` | Match when pins > value       |
| `PatternLessThan`   | `0x06` | Match when pins < value       |

## Using Triggers with Scans

Enable triggering via the scan options:

```go
cfg := usb1808.AnalogInScanConfig{
    Channels: []int{0, 1, 2, 3},
    Rate:     100000.0,
    Count:    1000,
    Options:  usb1808.ScanOptExternalTrigger,  // Wait for trigger to start.
}
```

For pattern detection triggering:

```go
cfg.Options = usb1808.ScanOptPatternDetection
```

For retriggered scans (acquire `RetrigCount` samples per trigger):

```go
cfg.Options = usb1808.ScanOptExternalTrigger | usb1808.ScanOptRetriggerMode
cfg.RetrigCount = 100  // 100 scans per trigger event.
```
