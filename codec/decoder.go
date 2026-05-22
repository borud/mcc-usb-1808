// Package codec decodes raw scan data into typed values (voltages, digital, counters).
package codec

import (
	"errors"
	"iter"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// ErrNoChannels is returned by [NewDecoder] when the channel list is empty.
var ErrNoChannels = errors.New("codec: no channels")

// Decoder decodes raw scan chunks into frames and typed values.
type Decoder struct {
	channels  []device.ChannelConfig
	coeffs    []voltCoeff
	// frameSize is the number of bytes per frame.
	frameSize int
}

// NewDecoder creates a Decoder for the given channel configuration and calibration table.
// Returns [ErrNoChannels] if channels is empty.
func NewDecoder(channels []device.ChannelConfig, cal device.CalibrationTable) (*Decoder, error) {
	if len(channels) == 0 {
		return nil, ErrNoChannels
	}
	ch := make([]device.ChannelConfig, len(channels))
	copy(ch, channels)
	return &Decoder{
		channels:  ch,
		coeffs:    makeCoeffs(ch, cal),
		frameSize: len(ch) * 4,
	}, nil
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
			coeffs:   d.coeffs,
			channels: d.channels,
		}
		for off := 0; off+d.frameSize <= len(chunk); off += d.frameSize {
			f.data = chunk[off : off+d.frameSize]
			if !yield(f) {
				return
			}
		}
	}
}

// DecodeVoltages decodes an entire chunk into calibrated voltages, writing
// them sequentially into dst (frame0_ch0, frame0_ch1, ..., frame1_ch0, ...).
// Returns the number of frames decoded. The caller must ensure dst has room
// for at least NumFrames(chunk) * len(channels) elements.
func (d *Decoder) DecodeVoltages(chunk []byte, dst []float64) int {
	numCh := len(d.coeffs)
	frameSize := d.frameSize
	coeffs := d.coeffs
	n := 0

	for off := 0; off+frameSize <= len(chunk); off += frameSize {
		for i := range numCh {
			c := &coeffs[i]
			raw := uint32(chunk[off+i*4]) |
				uint32(chunk[off+i*4+1])<<8 |
				uint32(chunk[off+i*4+2])<<16 |
				uint32(chunk[off+i*4+3])<<24
			if !c.analog {
				dst[n] = float64(raw)
			} else {
				v := float64(raw&0x3FFFF)*c.scale + c.offset
				dst[n] = max(c.clampLo, min(c.clampHi, v))
			}
			n++
		}
	}
	return n / numCh
}

// NumFrames returns the number of complete frames in a chunk.
func (d *Decoder) NumFrames(chunk []byte) int {
	return len(chunk) / d.frameSize
}

// MaxSamples returns the maximum number of float64 values that
// [Decoder.DecodeVoltages] can produce from a single chunk. Use this
// to pre-allocate a reusable destination buffer:
//
//	dst := make([]float64, dec.MaxSamples())
//	for chunk := range scan.Chunks() {
//	    nFrames := dec.DecodeVoltages(chunk.Data, dst)
//	    // use dst[:nFrames*numChannels]
//	}
func (d *Decoder) MaxSamples() int {
	return (device.MaxTransferBytes / d.frameSize) * len(d.coeffs)
}
