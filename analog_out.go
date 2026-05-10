package usb1808

import (
	"fmt"
	"math"

	"github.com/borud/mcc-usb-1808/internal/wire"
)

// AnalogOut writes a voltage to the specified analog output channel (0 or 1).
// The voltage is converted to a calibrated 16-bit DAC value.
func (d *Device) AnalogOut(channel int, voltage float64) error {
	if channel < 0 || channel >= NumAOutChannels {
		return fmt.Errorf("%w: %d", ErrInvalidChannel, channel)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check that output scan is not running.
	statusData, err := d.transport.ControlIn(cmdStatus, 0, 0, 2)
	if err != nil {
		return err
	}
	if Status(wire.Uint16LE(statusData)).AOutScanRunning() {
		return ErrAOutScanRunning
	}

	value := d.VoltsToAnalogOut(voltage, channel)
	return d.transport.ControlOut(cmdAOut, uint16(value), uint16(channel), nil)
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

// VoltsToAnalogOut converts a voltage to a 16-bit DAC value, applying calibration.
func (d *Device) VoltsToAnalogOut(voltage float64, channel int) uint16 {
	raw := voltage/10.0*32768.0 + 32768.0

	cal := raw*float64(d.calAOut[channel].Slope) + float64(d.calAOut[channel].Offset)

	if cal < 0 {
		cal = 0
	}
	if cal > 65535 {
		cal = 65535
	}
	return uint16(math.Round(cal))
}
