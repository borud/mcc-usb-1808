package device

import (
	"fmt"

	"github.com/borud/mcc-usb-1808/v4/wire"
)

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

// FPGAVersion reads the FPGA firmware version. Returns (major, minor).
func (d *Device) FPGAVersion() (uint8, uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdFPGAVersion, 0, 0, 2)
	if err != nil {
		return 0, 0, err
	}
	ver := wire.Uint16LE(data)
	return uint8(ver >> 8), uint8(ver & 0xFF), nil
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

// AnalogOutRaw writes a raw 16-bit value to the specified analog output channel.
func (d *Device) AnalogOutRaw(channel int, value uint16) error {
	if channel < 0 || channel >= NumAOutChannels {
		return fmt.Errorf("%w: %d", ErrInvalidChannel, channel)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdAOut, uint16(value), uint16(channel), nil)
}

// SetTriggerConfig writes the trigger configuration byte.
// Bit 0: mode (0=level, 1=edge). Bit 1: polarity (0=low/falling, 1=high/rising).
func (d *Device) SetTriggerConfig(config uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdTriggerConfig, 0, 0, []byte{config})
}
