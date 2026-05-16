package usb1808

import (
	"fmt"
	"math"

	"github.com/borud/mcc-usb-1808/v3/internal/wire"
)

// AnalogInChannelConfig holds the range and mode for a single analog input channel.
type AnalogInChannelConfig struct {
	Channel int
	Range   Range
	Mode    InputMode
}

// ConfigureAnalogIn configures the range and input mode for each analog input channel.
// Exactly 8 config bytes are sent (one per channel).
func (d *Device) ConfigureAnalogIn(configs []AnalogInChannelConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	buf := make([]byte, NumAInChannels)
	for _, c := range configs {
		if c.Channel < 0 || c.Channel >= NumAInChannels {
			return fmt.Errorf("%w: %d", ErrInvalidChannel, c.Channel)
		}
		if c.Mode == 2 {
			return fmt.Errorf("%w: mode code 2 is undefined", ErrInvalidMode)
		}
		buf[c.Channel] = uint8(c.Range) | (uint8(c.Mode) << 2)
		d.ainRanges[c.Channel] = c.Range
	}
	return d.transport.ControlOut(cmdADCSetup, 0, 0, buf)
}

// AnalogInConfig reads the current ADC configuration for all 8 channels.
func (d *Device) AnalogInConfig() ([NumAInChannels]AnalogInChannelConfig, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var configs [NumAInChannels]AnalogInChannelConfig
	data, err := d.transport.ControlIn(cmdADCSetup, 0, 0, NumAInChannels)
	if err != nil {
		return configs, err
	}
	for i, b := range data {
		configs[i] = AnalogInChannelConfig{
			Channel: i,
			Range:   Range(b & 0x03),
			Mode:    InputMode((b >> 2) & 0x03),
		}
	}
	return configs, nil
}

// AnalogInRaw performs a single asynchronous read of all 8 analog input channels.
// Returns the raw 18-bit values as uint32.
func (d *Device) AnalogInRaw() ([NumAInChannels]uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var result [NumAInChannels]uint32
	data, err := d.transport.ControlIn(cmdAIn, 0, 0, 32)
	if err != nil {
		return result, err
	}
	for i := 0; i < NumAInChannels; i++ {
		result[i] = wire.Uint32LE(data[i*4 : i*4+4])
	}
	return result, nil
}

// AnalogIn reads all 8 analog input channels and returns calibrated voltages.
// Channels must be configured first with ConfigureAnalogIn.
func (d *Device) AnalogIn() ([NumAInChannels]float64, error) {
	raw, err := d.AnalogInRaw()
	if err != nil {
		return [NumAInChannels]float64{}, err
	}
	var result [NumAInChannels]float64
	for i, v := range raw {
		result[i] = d.AnalogInToVolts(v, i, d.ainRanges[i])
	}
	return result, nil
}

// AnalogInToVolts converts a raw 18-bit ADC value to voltage, applying calibration.
func (d *Device) AnalogInToVolts(raw uint32, channel int, r Range) float64 {
	raw18 := raw & 0x3FFFF

	cal := float64(raw18)*float64(d.calAIn[channel][r].Slope) + float64(d.calAIn[channel][r].Offset)

	// Clamp for unipolar ranges.
	if r >= UP10V {
		if cal < 0 {
			cal = 0
		}
		if cal > 262143 {
			cal = 262143
		}
	}
	cal = math.Round(cal)

	switch r {
	case BP10V:
		return (cal - 131072.0) * 10.0 / 131072.0
	case BP5V:
		return (cal - 131072.0) * 5.0 / 131072.0
	case UP10V:
		return cal * 10.0 / 262143.0
	case UP5V:
		return cal * 5.0 / 262143.0
	}
	return 0
}
