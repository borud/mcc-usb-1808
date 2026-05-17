package capture

import "github.com/borud/mcc-usb-1808/v4/device"

// Frame holds one scan's worth of sampled data.
//
// For [RawUint32] format, [Frame.RawValues] returns the stored uint32 values
// and [Frame.Values] applies calibration to produce float64 voltages.
//
// For [calibratedFloat64] format, [Frame.RawValues] returns nil and
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
// Returns nil if the data format is [calibratedFloat64].
func (f *Frame) RawValues() []uint32 {
	return f.raw
}

// Values returns float64 values for all channels in the frame.
//
// For [calibratedFloat64] format this returns the stored values directly.
// For [RawUint32] format this applies the calibration coefficients from the
// file header to produce voltages for analog channels; non-analog channels
// (digital, counter, encoder) are returned as float64(raw).
//
// The returned slice is reused between calls; copy if you need to retain it.
func (f *Frame) Values() []float64 {
	if f.header.Format == calibratedFloat64 {
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
	cal := device.Calibration{Slope: ch.Cal.Slope, Offset: ch.Cal.Offset}
	return cal.ToVolts(raw, device.Range(ch.Range))
}
