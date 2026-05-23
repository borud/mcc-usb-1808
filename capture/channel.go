package capture

import "github.com/borud/mcc-usb-1808/v4/device"

// Channel describes a single channel in the capture.
type Channel struct {
	// Index is the hardware channel index.
	Index int `json:"index"`
	// Type is the channel type.
	Type device.ChannelType `json:"type"`
	// Range is the voltage range code.
	Range device.Range `json:"range,omitempty"`
	// Name is an optional human-readable label.
	Name string `json:"name,omitempty"`
	// Cal holds calibration coefficients. Analog + RawUint32 only.
	Cal *device.Calibration `json:"cal,omitempty"`
}
