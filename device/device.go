package device

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/borud/mcc-usb-1808/v4/transport"
)

// Option configures a Device at open time.
type Option func(*Device)

// KeepAOutOnClose disables the default behavior of zeroing both DAC channels
// when the device is closed. Use this to hold the last output value.
func KeepAOutOnClose() Option {
	return func(d *Device) { d.keepAOutOnClose = true }
}

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

	initialized     atomic.Bool
	keepAOutOnClose bool

	// Selection fields: set by WithModel / AtAddress, consumed once by Open.
	selectModel  Model
	selectBus    uint8
	selectAddr   uint8
	selectByAddr bool
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

// WithModel selects the first device of the given model variant.
func WithModel(m Model) Option {
	return func(d *Device) { d.selectModel = m }
}

// AtAddress selects the device at the given USB bus and address.
// Takes precedence over WithModel when both are provided.
func AtAddress(bus, addr uint8) Option {
	return func(d *Device) {
		d.selectBus = bus
		d.selectAddr = addr
		d.selectByAddr = true
	}
}

// Open opens a USB-1808 or USB-1808X device. Without selection options it
// returns the first device found (USB-1808X preferred). Use AtAddress or
// WithModel to narrow the selection.
func Open(opts ...Option) (*Device, error) {
	d := &Device{log: slog.Default()}
	for _, opt := range opts {
		opt(d)
	}

	switch {
	case d.selectByAddr:
		infos, err := Discover()
		if err != nil {
			return nil, err
		}
		found := false
		for _, info := range infos {
			if info.Bus == d.selectBus && info.Address == d.selectAddr {
				d.model = info.Model
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: no device at bus %d address %d", ErrDeviceNotFound, d.selectBus, d.selectAddr)
		}
		t, err := transport.OpenLibUSBAt(VendorID, d.selectBus, d.selectAddr)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeviceNotFound, err)
		}
		d.transport = t

	case d.selectModel != 0:
		t, err := transport.OpenLibUSB(VendorID, uint16(d.selectModel))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeviceNotFound, err)
		}
		d.transport = t
		d.model = d.selectModel

	default:
		for _, pid := range []uint16{PID1808X, PID1808} {
			t, err := transport.OpenLibUSB(VendorID, pid)
			if err != nil {
				continue
			}
			d.transport = t
			d.model = Model(pid)
			return d, nil
		}
		return nil, ErrDeviceNotFound
	}

	return d, nil
}

// NewDevice creates a Device using the given transport. This is primarily
// useful for testing with a mock transport.
func NewDevice(t transport.Transport, model Model, opts ...Option) *Device {
	d := &Device{
		transport: t,
		model:     model,
		log:       slog.Default(),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
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

// Close releases the USB device. By default it zeroes both DAC channels
// before closing. Use KeepAOutOnClose to suppress this.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.initialized.Load() {
		_ = d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
		_ = d.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
		if !d.keepAOutOnClose {
			for ch := range NumAOutChannels {
				code := d.VoltsToAOutRaw(ch, 0.0)
				_ = d.transport.ControlOut(cmdAOut, uint16(code), uint16(ch), nil)
			}
		}
	}
	d.initialized.Store(false)
	return d.transport.Close()
}
