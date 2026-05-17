# Errors

The library defines sentinel errors for common failure conditions. All are
declared as package-level `var` values in the `device` package and can be
matched with `errors.Is`.

| Error                  | Description                                         |
|------------------------|-----------------------------------------------------|
| `ErrDeviceNotFound`    | No USB-1808 or USB-1808X found on the bus.          |
| `ErrFPGANotConfigured` | FPGA image failed to load or is not present.        |
| `ErrScanOverrun`       | Analog input FIFO overrun (host read too slowly).   |
| `ErrScanUnderrun`      | Analog output FIFO underrun (host wrote too slowly).|
| `ErrScanRunning`       | A scan is already in progress.                      |
| `ErrInvalidChannel`    | Channel number out of valid range.                  |
| `ErrInvalidRange`      | Unrecognized voltage range code.                    |
| `ErrInvalidMode`       | Unrecognized input mode (mode 2 is undefined).      |
| `ErrTransferFailed`    | USB transfer did not complete.                      |
| `ErrTimeout`           | USB transfer timed out.                             |

## Error Wrapping

Some errors include additional context via `fmt.Errorf` with `%w`:

```go
h, err := dev.CreateScan(cfg)
if errors.Is(err, device.ErrInvalidChannel) {
    // handle bad channel number
}
```

For example, `ErrDeviceNotFound` from `OpenModel` wraps the underlying libusb
error, and `Init` wraps calibration table errors with descriptive prefixes.
