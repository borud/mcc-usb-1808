# Getting Started

## Prerequisites

- Go 1.24 or later
- libusb 1.0 development files

### macOS

```sh
brew install libusb
```

### Debian / Ubuntu

```sh
apt install libusb-1.0-0-dev
```

## Installation

```sh
go get github.com/borud/mcc-usb-1808
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/borud/mcc-usb-1808"
)

func main() {
    dev, err := usb1808.Open()
    if err != nil {
        log.Fatal(err)
    }
    defer dev.Close()

    if err := dev.Init(); err != nil {
        log.Fatal(err)
    }

    serial, _ := dev.SerialNumber()
    fmt.Printf("Model: %s  Serial: %s\n", dev.Model(), serial)
}
```

## Device Lifecycle

### Opening

`Open` discovers and opens the first USB-1808 or USB-1808X it finds (tries
1808X first):

```go
dev, err := usb1808.Open()
```

To open a specific model:

```go
dev, err := usb1808.OpenModel(usb1808.USB1808X)
```

### Initialization

`Init` must be called before performing analog reads or scans. It loads the
FPGA image (if needed) and builds the calibration tables from EEPROM.

The first `Init` after the device is powered on takes a few seconds while the
FPGA image is streamed. Subsequent calls skip the FPGA load.

```go
if err := dev.Init(); err != nil {
    log.Fatal(err)
}
```

### Logging

The device logs FPGA loading progress and diagnostics via `log/slog`. By
default it uses `slog.Default()`. To use a custom logger:

```go
dev.SetLogger(myLogger)
```

### Closing

`Close` stops any running scans and releases the USB device:

```go
dev.Close()
```

### Status and Identity

```go
status, _ := dev.Status()
fmt.Println("FPGA ready:", status.FPGAConfigured())

major, minor, _ := dev.FPGAVersion()
fmt.Printf("FPGA firmware: %d.%d\n", major, minor)

serial, _ := dev.SerialNumber()
fmt.Println("Serial:", serial)
```

### Utility

```go
dev.BlinkLED(5)  // Blink the LED 5 times.
dev.Reset()      // Reset the device.
```

## Thread Safety

Control and configuration methods (single reads, writes, status, configuration)
are safe for concurrent use and serialized by an internal mutex. A running scan
iterator (`ScanAnalogIn`, `ScanAnalogInRaw`, `ScanAnalogInBulk`) owns the scan
until stopped and should not be used concurrently with other scan operations.
