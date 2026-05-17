package device

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/borud/mcc-usb-1808/v4/transport"
)

// Device represents an open USB-1808 or USB-1808X.
// All methods are safe for concurrent use.
type Device struct {
	transport transport.Transport
	model     Model
	mu        sync.Mutex
	log       *slog.Logger

	// Calibration tables, populated by Init.
	calAIn  CalibrationTable
	calAOut [NumAOutChannels]Calibration

	initialized atomic.Bool
}

// Open discovers and opens the first USB-1808 or USB-1808X device found.
func Open() (*Device, error) {
	for _, pid := range []uint16{PID1808X, PID1808} {
		t, err := transport.OpenLibUSB(VendorID, pid)
		if err != nil {
			continue
		}
		return &Device{
			transport: t,
			model:     Model(pid),
			log:       slog.Default(),
		}, nil
	}
	return nil, ErrDeviceNotFound
}

// OpenModel opens a specific model variant.
func OpenModel(model Model) (*Device, error) {
	t, err := transport.OpenLibUSB(VendorID, uint16(model))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDeviceNotFound, err)
	}
	return &Device{
		transport: t,
		model:     model,
		log:       slog.Default(),
	}, nil
}

// NewDevice creates a Device using the given transport. This is primarily
// useful for testing with a mock transport.
func NewDevice(t transport.Transport, model Model) *Device {
	return &Device{
		transport: t,
		model:     model,
		log:       slog.Default(),
	}
}

// SetLogger sets the logger used by the device.
func (d *Device) SetLogger(l *slog.Logger) {
	if l == nil {
		l = slog.Default()
	}
	d.mu.Lock()
	d.log = l
	d.mu.Unlock()
}

// Model returns the device model.
func (d *Device) Model() Model {
	return d.model
}

// AsyncBulkSupported reports whether the underlying transport supports
// async bulk transfer rings.
func (d *Device) AsyncBulkSupported() bool {
	_, ok := d.transport.(transport.AsyncBulkReader)
	return ok
}

// Close releases the USB device.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.initialized.Load() {
		_ = d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
		_ = d.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
	}
	d.initialized.Store(false)
	return d.transport.Close()
}
