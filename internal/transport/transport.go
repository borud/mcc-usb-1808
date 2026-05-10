// Package transport defines the low-level USB transport interface
// used by the usb1808 driver. This abstraction enables testing the
// entire protocol stack without hardware.
package transport

import "time"

// Transport provides low-level USB communication with a device.
// BulkRead and BulkReadInto are safe for concurrent use with control
// transfers (libusb 1.0 is thread-safe). Control transfers are
// serialized by the implementation for logical ordering.
type Transport interface {
	// ControlOut sends a vendor control transfer from host to device.
	ControlOut(request uint8, wValue, wIndex uint16, data []byte) error

	// ControlIn sends a vendor control transfer from device to host
	// and returns the received data.
	ControlIn(request uint8, wValue, wIndex uint16, length int) ([]byte, error)

	// BulkRead reads data from a bulk IN endpoint.
	BulkRead(endpoint uint8, length int, timeout time.Duration) ([]byte, error)

	// BulkReadInto reads data from a bulk IN endpoint into a caller-provided
	// buffer. Returns the number of bytes actually transferred.
	BulkReadInto(endpoint uint8, buf []byte, timeout time.Duration) (int, error)

	// BulkWrite writes data to a bulk OUT endpoint.
	BulkWrite(endpoint uint8, data []byte, timeout time.Duration) (int, error)

	// Close releases the USB device and any associated resources.
	Close() error
}
