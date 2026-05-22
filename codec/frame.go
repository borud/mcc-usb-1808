package codec

import (
	"encoding/binary"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// Frame is a view into one scan's worth of raw data. It provides methods
// to extract typed sample values. A Frame is only valid until the underlying
// chunk buffer is reused.
type Frame struct {
	data     []byte
	coeffs   []voltCoeff
	channels []device.ChannelConfig
}

// Voltage converts the sample at channelIdx to a calibrated voltage.
// For non-analog channels, returns float64(raw).
func (f *Frame) Voltage(channelIdx int) float64 {
	c := &f.coeffs[channelIdx]
	raw := binary.LittleEndian.Uint32(f.data[channelIdx*4:])
	if !c.analog {
		return float64(raw)
	}
	v := float64(raw&0x3FFFF)*c.scale + c.offset
	return max(c.clampLo, min(c.clampHi, v))
}

// RawUint32 returns the raw uint32 value at channelIdx.
func (f *Frame) RawUint32(channelIdx int) uint32 {
	return binary.LittleEndian.Uint32(f.data[channelIdx*4:])
}

// Digital returns the digital I/O byte from the frame.
// It searches for the first DIO channel; returns 0 if none.
func (f *Frame) Digital() uint8 {
	for i, ch := range f.channels {
		if ch.Type == device.ChannelTypeDIO {
			return uint8(binary.LittleEndian.Uint32(f.data[i*4:]))
		}
	}
	return 0
}

// Data returns the raw underlying byte slice.
func (f *Frame) Data() []byte {
	return f.data
}
