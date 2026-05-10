// Package transport defines the low-level USB transport interface
// used by the usb1808 driver. This abstraction enables testing the
// entire protocol stack without hardware.
package transport

import "time"

// Transport provides low-level USB communication with a device.
// All methods are safe for concurrent use only if the implementation
// documents thread safety.
type Transport interface {
	// ControlOut sends a vendor control transfer from host to device.
	ControlOut(request uint8, wValue, wIndex uint16, data []byte) error

	// ControlIn sends a vendor control transfer from device to host
	// and returns the received data.
	ControlIn(request uint8, wValue, wIndex uint16, length int) ([]byte, error)

	// BulkRead reads data from a bulk IN endpoint.
	BulkRead(endpoint uint8, length int, timeout time.Duration) ([]byte, error)

	// BulkWrite writes data to a bulk OUT endpoint.
	BulkWrite(endpoint uint8, data []byte, timeout time.Duration) (int, error)

	// Close releases the USB device and any associated resources.
	Close() error
}
