package device

import (
	"fmt"
	"math"
)

// AnalogOut writes a calibrated voltage to the specified DAC channel.
// Refuses with ErrAOutScanRunning if an output scan is active.
// channel must be 0 or 1; volts is clamped to ±10 V by the hardware.
func (d *Device) AnalogOut(channel int, volts float64) error {
	if channel < 0 || channel >= NumAOutChannels {
		return fmt.Errorf("%w: %d", ErrInvalidChannel, channel)
	}
	code := d.VoltsToAOutRaw(channel, volts)
	return d.AnalogOutRaw(channel, code)
}

// AnalogOutRaw writes a raw 16-bit DAC code (offset binary, 0x8000 = 0 V).
// Refuses with ErrAOutScanRunning if an output scan is active.
func (d *Device) AnalogOutRaw(channel int, value uint16) error {
	if channel < 0 || channel >= NumAOutChannels {
		return fmt.Errorf("%w: %d", ErrInvalidChannel, channel)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st, err := d.status()
	if err != nil {
		return err
	}
	if st.AOutScanRunning() {
		return ErrAOutScanRunning
	}
	return d.transport.ControlOut(cmdAOut, uint16(value), uint16(channel), nil)
}

// VoltsToAOutRaw converts a voltage to a calibrated 16-bit DAC code.
// The DAC range is ±10 V (fixed). Codes are offset binary: 0x8000 = 0 V,
// 0x0000 = −10 V, 0xFFFF = +10 V. Calibration coefficients from EEPROM
// are applied.
func (d *Device) VoltsToAOutRaw(channel int, volts float64) uint16 {
	// Pre-calibration: raw maps volts → nominal offset-binary code.
	raw := volts/10.0*32768.0 + 32768.0
	cal := raw*float64(d.calAOut[channel].Slope) + float64(d.calAOut[channel].Offset)
	cal = math.Round(cal)
	if cal < 0 {
		cal = 0
	}
	if cal > 65535 {
		cal = 65535
	}
	return uint16(cal)
}
