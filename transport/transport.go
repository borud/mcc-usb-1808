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

// RingResult holds one completed async bulk transfer.
type RingResult struct {
	Data []byte
	Err  error
}

// BulkRing is the interface for an async bulk transfer ring.
// Implementations deliver completed transfers via Results and can be
// stopped with Stop.
type BulkRing interface {
	Results() <-chan RingResult
	Stop()
}

// AsyncBulkReader is an optional interface that Transport implementations
// can provide to support async bulk transfer rings.
type AsyncBulkReader interface {
	NewBulkRing(endpoint uint8, bufSize, count, depth int, timeoutMs uint) (BulkRing, error)
}

// BulkOutSource fills the next staging buffer with bytes to send.
// Implementations return (n, nil) on progress, (n, io.EOF) when exhausted
// (n may be 0), or (0, err) on fatal error. Returning (0, nil) signals
// "no data yet" and is treated as backpressure — the ring retries.
type BulkOutSource interface {
	Fill(buf []byte) (n int, err error)
}

// BulkOutRingStats holds lifetime counters for a BulkOutRing.
type BulkOutRingStats struct {
	Submitted uint64
	Completed uint64
	BytesSent uint64
}

// BulkOutRing is the interface for an async bulk OUT transfer ring.
type BulkOutRing interface {
	// Done closes when the source returns io.EOF and all in-flight transfers
	// have completed.
	Done() <-chan struct{}
	// Err returns any error from the source or libusb.
	Err() error
	// Stop cancels in-flight transfers and frees resources. Always call Stop
	// when finished with the ring, even after Done closes.
	Stop()
	// Stats returns lifetime counters.
	Stats() BulkOutRingStats
}

// AsyncBulkWriter is an optional interface that Transport implementations
// can provide to support async bulk OUT transfer rings.
type AsyncBulkWriter interface {
	NewBulkOutRing(endpoint uint8, bufSize, count int, source BulkOutSource, timeoutMs uint) (BulkOutRing, error)
}

// DeviceInfo describes a discovered USB device before it is opened.
type DeviceInfo struct {
	VendorID  uint16
	ProductID uint16
	Bus       uint8
	Address   uint8
}
