package codec

import "github.com/borud/mcc-usb-1808/v4/device"

// RawToVolts converts a raw 18-bit ADC value to voltage, applying calibration.
// This is a convenience wrapper around [device.Calibration.ToVolts].
func RawToVolts(raw uint32, r device.Range, cal device.Calibration) float64 {
	return cal.ToVolts(raw, r)
}
