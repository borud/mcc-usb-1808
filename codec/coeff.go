package codec

import (
	"math"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// voltCoeff holds precomputed voltage conversion coefficients for one channel.
// For analog channels the conversion is: voltage = raw18 * scale + offset,
// clamped to [clampLo, clampHi] for unipolar ranges.
type voltCoeff struct {
	scale   float64
	offset  float64
	clampLo float64
	clampHi float64
	analog  bool
}

// makeCoeffs precomputes per-channel voltage conversion coefficients from the
// device calibration table.  The result lets the decoder convert a raw 18-bit
// ADC sample to volts with a single multiply-add (raw*scale + offset) instead
// of branching on range and applying calibration on every sample.
//
// For bipolar ranges (BP10V, BP5V) the formula folds the calibration slope/offset
// and the midscale subtraction into one linear transform with no clamping.
// For unipolar ranges (UP10V, UP5V) the transform maps [0, ADCFullScale] to
// [0, rangeMax] and clamps the output to that interval.
//
// Non-analog channels (DIO, counter, encoder) get a zero-valued voltCoeff with
// analog=false so the decoder can skip voltage conversion for them.
func makeCoeffs(channels []device.ChannelConfig, cal device.CalibrationTable) []voltCoeff {
	coeffs := make([]voltCoeff, len(channels))
	for i, ch := range channels {
		if ch.Type != device.ChannelTypeAnalog {
			continue
		}
		c := cal[ch.Index][ch.Range]
		slope := float64(c.Slope)
		off := float64(c.Offset)

		var vc voltCoeff
		vc.analog = true

		switch ch.Range {
		case device.BP10V:
			vc.scale = slope * 10.0 / device.ADCMidscale
			vc.offset = (off - device.ADCMidscale) * 10.0 / device.ADCMidscale
			vc.clampLo = -math.MaxFloat64
			vc.clampHi = math.MaxFloat64
		case device.BP5V:
			vc.scale = slope * 5.0 / device.ADCMidscale
			vc.offset = (off - device.ADCMidscale) * 5.0 / device.ADCMidscale
			vc.clampLo = -math.MaxFloat64
			vc.clampHi = math.MaxFloat64
		case device.UP10V:
			vc.scale = slope * 10.0 / device.ADCFullScale
			vc.offset = off * 10.0 / device.ADCFullScale
			vc.clampLo = 0
			vc.clampHi = 10
		case device.UP5V:
			vc.scale = slope * 5.0 / device.ADCFullScale
			vc.offset = off * 5.0 / device.ADCFullScale
			vc.clampLo = 0
			vc.clampHi = 5
		}
		coeffs[i] = vc
	}
	return coeffs
}
