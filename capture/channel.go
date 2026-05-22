package capture

import "github.com/borud/mcc-usb-1808/v4/device"

// ChannelType identifies the kind of data source.
type ChannelType uint8

// ChannelType values.
const (
	// AnalogIn represents an analog input channel (channels 0-7).
	AnalogIn ChannelType = iota
	// DigitalIO represents the digital I/O port (channel 8).
	DigitalIO
	// Counter represents a counter channel (channels 9-10).
	Counter
	// Encoder represents a quadrature encoder channel (channels 11-12).
	Encoder
)

// CalEntry holds calibration coefficients for a single channel at its
// configured voltage range. These are the slope and offset values read
// from the device EEPROM.
type CalEntry struct {
	Slope  float32 `json:"slope"`
	Offset float32 `json:"offset"`
}

// Channel describes a single channel in the capture.
type Channel struct {
	// Index is the hardware channel index.
	Index int `json:"index"`
	// Type is the channel type.
	Type ChannelType `json:"type"`
	// Range is the voltage range code (0=BP10V, 1=BP5V, 2=UP10V, 3=UP5V). AnalogIn only.
	Range uint8 `json:"range,omitempty"`
	// Name is an optional human-readable label.
	Name string `json:"name,omitempty"`
	// Cal holds calibration coefficients. AnalogIn + RawUint32 only.
	Cal *CalEntry `json:"cal,omitempty"`
}

// ChannelTypeFromDevice converts a device.ChannelType to a capture.ChannelType.
func ChannelTypeFromDevice(t device.ChannelType) ChannelType {
	switch t {
	case device.ChannelTypeAnalog:
		return AnalogIn
	case device.ChannelTypeDIO:
		return DigitalIO
	case device.ChannelTypeCounter:
		return Counter
	case device.ChannelTypeEncoder:
		return Encoder
	default:
		return AnalogIn
	}
}
