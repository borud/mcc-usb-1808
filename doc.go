// Package usb1808 provides a Go driver for the Measurement Computing
// USB-1808 and USB-1808X multifunction DAQ devices.
//
// The primary entry point is the device sub-package which provides the
// Device type with Open, Init, CreateScan, and other hardware operations.
//
// # Package layout
//
//   - device/    — Device driver, scan handle, calibration
//   - codec/     — Decode raw scan bytes into voltages
//   - stream/    — Fan-out raw scan data to multiple consumers
//   - capture/   — Capture file format (write/read/export)
//   - transport/ — USB transport abstraction (libusb, mock)
//   - wire/      — Wire-level byte encoding helpers
//   - firmware/  — Embedded FPGA bitstream
//
// # Quick start
//
//	dev, err := device.Open()
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer dev.Close()
//
//	if err := dev.Init(); err != nil {
//		log.Fatal(err)
//	}
//
//	cfg := device.ScanConfig{
//		Channels: []device.ChannelConfig{
//			{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
//		},
//		Rate: 10000,
//	}
//	h, err := dev.CreateScan(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//	h.Start()
//	for chunk := range h.Chunks() {
//		// process raw bytes...
//	}
//	h.Stop()
package usb1808
