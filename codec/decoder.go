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
	buf       []float64
}

// VoltageTable is a decoded chunk of calibrated voltages arranged as rows
// of frames and columns of channels. It references the Decoder's internal
// buffer and is only valid until the next call to [Decoder.DecodeVoltages].
type VoltageTable struct {
	data []float64
	cols int
	rows int
}

// Frames returns the number of decoded frames.
func (t VoltageTable) Frames() int { return t.rows }

// Row returns the voltage values for frame i.
func (t VoltageTable) Row(i int) []float64 { return t.data[i*t.cols : (i+1)*t.cols] }

// NewDecoder creates a Decoder for the given channel configuration and calibration table.
// Returns [ErrNoChannels] if channels is empty.
func NewDecoder(channels []device.ChannelConfig, cal device.CalibrationTable) (*Decoder, error) {
	if len(channels) == 0 {
		return nil, ErrNoChannels
	}
	ch := make([]device.ChannelConfig, len(channels))
	copy(ch, channels)
	frameSize := len(ch) * 4
	numCh := len(ch)
	maxFrames := device.MaxTransferBytes / frameSize
	return &Decoder{
		channels:  ch,
		coeffs:    makeCoeffs(ch, cal),
		frameSize: frameSize,
		buf:       make([]float64, maxFrames*numCh),
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

// DecodeVoltages bulk-decodes a raw chunk into a [VoltageTable]. The returned
// table references the Decoder's internal buffer and is valid until the next
// DecodeVoltages call.
func (d *Decoder) DecodeVoltages(chunk []byte) VoltageTable {
	numCh := len(d.coeffs)
	nFrames := d.DecodeVoltagesInto(chunk, d.buf)
	return VoltageTable{data: d.buf[:nFrames*numCh], cols: numCh, rows: nFrames}
}

// DecodeVoltagesInto decodes an entire chunk into calibrated voltages, writing
// them sequentially into dst (frame0_ch0, frame0_ch1, ..., frame1_ch0, ...).
// Returns the number of frames decoded. The caller must ensure dst has room
// for at least NumFrames(chunk) * len(channels) elements.
func (d *Decoder) DecodeVoltagesInto(chunk []byte, dst []float64) int {
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
// [Decoder.DecodeVoltagesInto] can produce from a single chunk. Use this
// to pre-allocate a reusable destination buffer:
//
//	dst := make([]float64, dec.MaxSamples())
//	for chunk := range scan.Chunks() {
//	    nFrames := dec.DecodeVoltagesInto(chunk.Data, dst)
//	    // use dst[:nFrames*numChannels]
//	}
func (d *Decoder) MaxSamples() int {
	return (device.MaxTransferBytes / d.frameSize) * len(d.coeffs)
}
