// Package codec decodes raw scan data into typed values (voltages, digital, counters).
package codec

import (
	"iter"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// Decoder decodes raw scan chunks into frames and typed values.
type Decoder struct {
	channels  []device.ChannelConfig
	cal       device.CalibrationTable
	frameSize int // bytes per frame
}

// NewDecoder creates a Decoder for the given channel configuration and calibration table.
func NewDecoder(channels []device.ChannelConfig, cal device.CalibrationTable) *Decoder {
	return &Decoder{
		channels:  channels,
		cal:       cal,
		frameSize: len(channels) * 4,
	}
}

// FrameSize returns the number of bytes per frame.
func (d *Decoder) FrameSize() int {
	return d.frameSize
}

// Frames returns an iterator over frames in the given raw chunk.
// The chunk must be aligned to frame boundaries (len(chunk) % frameSize == 0).
// Each yielded Frame references a subslice of chunk — it is only valid until
// the chunk is reused.
func (d *Decoder) Frames(chunk []byte) iter.Seq[*Frame] {
	return func(yield func(*Frame) bool) {
		f := &Frame{
			channels: d.channels,
			cal:      &d.cal,
		}
		for off := 0; off+d.frameSize <= len(chunk); off += d.frameSize {
			f.data = chunk[off : off+d.frameSize]
			if !yield(f) {
				return
			}
		}
	}
}
