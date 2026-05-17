// Package codec decodes raw scan data into typed values (voltages, digital, counters).
package codec

import (
	"encoding/binary"
	"iter"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// Frame is a view into raw scan data representing one scan's samples.
// It is a byte slice of length nChannels * 4.
type Frame []byte

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
// Each yielded Frame is a subslice of chunk — it is only valid until chunk is reused.
func (d *Decoder) Frames(chunk []byte) iter.Seq[Frame] {
	return func(yield func(Frame) bool) {
		for off := 0; off+d.frameSize <= len(chunk); off += d.frameSize {
			if !yield(Frame(chunk[off : off+d.frameSize])) {
				return
			}
		}
	}
}

// Voltage converts the sample at channelIdx in the frame to a calibrated voltage.
// For non-analog channels, returns float64(raw).
func (d *Decoder) Voltage(frame Frame, channelIdx int) float64 {
	ch := d.channels[channelIdx]
	raw := binary.LittleEndian.Uint32(frame[channelIdx*4 : channelIdx*4+4])

	if ch.Type != device.ChannelTypeAnalog {
		return float64(raw)
	}
	return RawToVolts(raw, ch.Range, d.cal[ch.Index][ch.Range])
}

// RawUint32 returns the raw uint32 value at channelIdx in the frame.
func (d *Decoder) RawUint32(frame Frame, channelIdx int) uint32 {
	return binary.LittleEndian.Uint32(frame[channelIdx*4 : channelIdx*4+4])
}

// Digital returns the digital I/O byte from the frame.
// It searches for the first DIO channel; returns 0 if none.
func (d *Decoder) Digital(frame Frame) uint8 {
	for i, ch := range d.channels {
		if ch.Type == device.ChannelTypeDIO {
			return uint8(binary.LittleEndian.Uint32(frame[i*4 : i*4+4]))
		}
	}
	return 0
}
