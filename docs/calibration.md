# Calibration and Device Info

## Factory Calibration

The USB-1808 stores factory calibration coefficients in EEPROM. `Init` reads
these automatically and applies them to all voltage conversions.

Each calibration entry is a slope/offset pair:

```go
type Calibration struct {
    Slope  float32
    Offset float32
}
```

- ADC: 8 channels x 4 ranges = 32 coefficient pairs (EEPROM 0x7000-0x70FF)
- DAC: 2 channels = 2 coefficient pairs (EEPROM 0x7100-0x710F)

## Accessing Calibration Tables

After `Init`, the calibration tables can be inspected:

```go
adcCal := dev.CalibrationTable()    // [8][4]Calibration
dacCal := dev.AnalogOutCalTable()   // [2]Calibration
```

## Calibration Date

Read the factory calibration date:

```go
calDate, err := dev.CalibrationDate()
fmt.Println(calDate.Format("2006-01-02 15:04:05"))
```

The date is stored at EEPROM address 0x7110 as 6 bytes: year offset from
2000, month, day, hour, minute, second.

## How Calibration Is Applied

**Analog input** (`Calibration.ToVolts`):

1. Mask raw value to 18 bits.
2. Apply: `cal = raw * slope + offset`
3. Clamp unipolar ranges to [0, 262143].
4. Round and convert to voltage based on range.

```go
cal := dev.CalibrationTable()
v := cal[channel][rangeCode].ToVolts(rawValue, rangeCode)
```

## Device Identity

These methods work without calling `Init`:

```go
model := dev.Model()               // USB1808 or USB1808X
serial, err := dev.SerialNumber()   // 8-byte ASCII serial number
major, minor, err := dev.FPGAVersion()
status, err := dev.Status()
```

### Status Bits

| Method              | Description                          |
|---------------------|--------------------------------------|
| `AInScanRunning()`  | Analog input scan pacer is active.   |
| `AInScanOverrun()`  | Analog input FIFO overrun.           |
| `AOutScanRunning()` | Analog output scan is active.        |
| `AOutScanUnderrun()`| Analog output FIFO underrun.         |
| `AInScanDone()`     | Analog input scan completed.         |
| `AOutScanDone()`    | Analog output scan completed.        |
| `FPGAConfigured()`  | FPGA is loaded and ready.            |
| `FPGAConfigMode()`  | Device is in FPGA configuration mode.|
