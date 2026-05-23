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

// Info describes a discovered USB-1808/1808X device.
type Info struct {
	Model   Model
	Bus     uint8
	Address uint8
}

// Discover returns all connected USB-1808/1808X devices.
func Discover() ([]Info, error) {
	infos, err := transport.Discover(VendorID, []uint16{PID1808, PID1808X})
	if err != nil {
		return nil, err
	}
	result := make([]Info, len(infos))
	for i, info := range infos {
		result[i] = Info{
			Model:   Model(info.ProductID),
			Bus:     info.Bus,
			Address: info.Address,
		}
	}
	return result, nil
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

// OpenAt opens the device at the given USB bus and address.
func OpenAt(bus, addr uint8) (*Device, error) {
	// Discover to determine the model at this location.
	infos, err := Discover()
	if err != nil {
		return nil, err
	}
	var model Model
	found := false
	for _, info := range infos {
		if info.Bus == bus && info.Address == addr {
			model = info.Model
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: no device at bus %d address %d", ErrDeviceNotFound, bus, addr)
	}

	t, err := transport.OpenLibUSBAt(VendorID, bus, addr)
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
