package codec

import (
	"math"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// RawToVolts converts a raw 18-bit ADC value to voltage, applying calibration.
func RawToVolts(raw uint32, r device.Range, cal device.Calibration) float64 {
	raw18 := raw & 0x3FFFF
	calibrated := float64(raw18)*float64(cal.Slope) + float64(cal.Offset)

	// Clamp for unipolar ranges.
	if r >= device.UP10V {
		calibrated = max(0, min(262143, calibrated))
	}
	calibrated = math.Round(calibrated)

	switch r {
	case device.BP10V:
		return (calibrated - 131072.0) * 10.0 / 131072.0
	case device.BP5V:
		return (calibrated - 131072.0) * 5.0 / 131072.0
	case device.UP10V:
		return calibrated * 10.0 / 262143.0
	case device.UP5V:
		return calibrated * 5.0 / 262143.0
	default:
		return float64(raw)
	}
}
