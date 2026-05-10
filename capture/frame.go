package capture

import "math"

// Frame holds one scan's worth of sampled data.
//
// For [RawUint32] format, [Frame.RawValues] returns the stored uint32 values
// and [Frame.Values] applies calibration to produce float64 voltages.
//
// For [CalibratedFloat64] format, [Frame.RawValues] returns nil and
// [Frame.Values] returns the stored float64 values directly.
//
// All returned slices are owned by the [Reader] and reused between calls.
// Copy any data you need to keep.
type Frame struct {
	raw    []uint32
	floats []float64
	header *Header
}

// RawValues returns the raw uint32 sample values.
// Returns nil if the data format is [CalibratedFloat64].
func (f *Frame) RawValues() []uint32 {
	return f.raw
}

// Values returns float64 values for all channels in the frame.
//
// For [CalibratedFloat64] format this returns the stored values directly.
// For [RawUint32] format this applies the calibration coefficients from the
// file header to produce voltages for analog channels; non-analog channels
// (digital, counter, encoder) are returned as float64(raw).
//
// The returned slice is reused between calls; copy if you need to retain it.
func (f *Frame) Values() []float64 {
	if f.header.Format == CalibratedFloat64 {
		return f.floats
	}
	for i, ch := range f.header.Channels {
		f.floats[i] = calibrate(f.raw[i], ch)
	}
	return f.floats
}

// calibrate converts a raw sample value to a calibrated float64 using the
// channel's calibration entry and voltage range. Non-analog channels and
// channels without calibration data are returned as float64(raw).
func calibrate(raw uint32, ch Channel) float64 {
	if ch.Type != AnalogIn || ch.Cal == nil {
		return float64(raw)
	}

	raw18 := raw & 0x3FFFF
	cal := float64(raw18)*float64(ch.Cal.Slope) + float64(ch.Cal.Offset)

	// Clamp for unipolar ranges.
	if ch.Range >= 2 {
		cal = max(0, min(262143, cal))
	}
	cal = math.Round(cal)

	switch ch.Range {
	case 0: // BP10V ±10V
		return (cal - 131072.0) * 10.0 / 131072.0
	case 1: // BP5V ±5V
		return (cal - 131072.0) * 5.0 / 131072.0
	case 2: // UP10V 0-10V
		return cal * 10.0 / 262143.0
	case 3: // UP5V 0-5V
		return cal * 5.0 / 262143.0
	default:
		return float64(raw)
	}
}
