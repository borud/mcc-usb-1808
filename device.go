package usb1808

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/borud/mcc-usb-1808/v3/internal/transport"
	"github.com/borud/mcc-usb-1808/v3/internal/wire"
)

// Calibration holds a single calibration coefficient pair read from EEPROM.
type Calibration struct {
	Slope  float32
	Offset float32
}

// Device represents an open USB-1808 or USB-1808X.
// All methods are safe for concurrent use.
type Device struct {
	transport transport.Transport
	model     Model
	mu        sync.Mutex
	log       *slog.Logger

	// Calibration tables, populated by Init.
	calAIn  [NumAInChannels][NumAInRanges]Calibration
	calAOut [NumAOutChannels]Calibration

	// Cached analog input ranges (set by ConfigureAnalogIn).
	ainRanges [NumAInChannels]Range

	// Cached scan queue (set by ConfigureAnalogInScan).
	ainScanQueue []int

	// Timer parameter cache (firmware returns wrong values on read).
	timerCache [NumTimers]TimerConfig

	initialized atomic.Bool
}

// Open discovers and opens the first USB-1808 or USB-1808X device found.
func Open() (*Device, error) {
	// Try USB-1808X first, then USB-1808.
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
func NewDevice(transport transport.Transport, model Model) *Device {
	return &Device{
		transport: transport,
		model:     model,
		log:       slog.Default(),
	}
}

// SetLogger sets the logger used by the device. If nil, slog.Default() is used.
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
// async bulk transfer rings (true for real hardware, false for mocks).
func (d *Device) AsyncBulkSupported() bool {
	_, ok := d.transport.(transport.AsyncBulkReader)
	return ok
}

// Close releases the USB device.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Best-effort cleanup: stop any running scans.
	if d.initialized.Load() {
		_ = d.transport.ControlOut(cmdAInScanStop, 0, 0, nil)
		_ = d.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
	}
	d.initialized.Store(false)
	return d.transport.Close()
}

// BlinkLED blinks the device LED the specified number of times.
func (d *Device) BlinkLED(count uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdBlinkLED, 0, 0, []byte{count})
}

// Reset resets the device.
func (d *Device) Reset() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.initialized.Store(false)
	return d.transport.ControlOut(cmdReset, 0, 0, nil)
}

// FPGAVersion reads the FPGA firmware version.
// Returns (major, minor).
func (d *Device) FPGAVersion() (uint8, uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdFPGAVersion, 0, 0, 2)
	if err != nil {
		return 0, 0, err
	}
	// High byte = major, low byte = minor.
	ver := wire.Uint16LE(data)
	return uint8(ver >> 8), uint8(ver & 0xFF), nil
}

// SerialNumber reads the 8-byte ASCII serial number.
func (d *Device) SerialNumber() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdSerial, 0, 0, 8)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
