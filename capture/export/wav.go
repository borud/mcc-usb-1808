package export

import (
	"encoding/binary"
	"io"
	"math"

	"github.com/borud/mcc-usb-1808/v4/capture"
)

// WAV writes all remaining frames from r as a WAV audio file to w.
//
// Each capture channel becomes one audio channel. Sample values are
// normalized to the [-1, +1] range and stored as 32-bit float PCM
// (IEEE 754). The WAV sample rate is taken from the capture header.
//
// This format is useful for visualizing signals in audio editors such
// as Audacity. Non-analog channels (digital, counter, encoder) are
// included as-is, scaled to [-1, +1] by dividing by 262143 (the 18-bit
// ADC full-scale value).
//
// WAV files have a 4 GiB size limit due to the 32-bit chunk size fields.
// For 8 channels at 100 kHz, this is roughly 22 minutes.
func WAV(w io.Writer, r *capture.Reader) error {
	h := r.Header()
	numCh := len(h.Channels)

	// Collect all frames into memory so we know the total data size
	// before writing the WAV header (WAV requires sizes upfront).
	var frames [][]float64
	for frame, err := range r.Frames() {
		if err != nil {
			return err
		}
		vals := make([]float64, numCh)
		copy(vals, frame.Values())
		frames = append(frames, vals)
	}

	numFrames := len(frames)
	bitsPerSample := 32
	bytesPerSample := bitsPerSample / 8
	blockAlign := numCh * bytesPerSample
	dataSize := numFrames * blockAlign
	sampleRate := uint32(h.SampleRate)

	// Compute normalization scale per channel.
	// For analog channels, find the max absolute value from the data.
	// For other channels, use the 18-bit full-scale value.
	scales := make([]float64, numCh)
	for i, ch := range h.Channels {
		if ch.Type != capture.AnalogIn {
			scales[i] = 262143.0
			continue
		}
		var maxAbs float64
		for _, f := range frames {
			if a := math.Abs(f[i]); a > maxAbs {
				maxAbs = a
			}
		}
		if maxAbs == 0 {
			maxAbs = 1.0
		}
		scales[i] = maxAbs
	}

	// Write RIFF/WAV header.
	riffSize := uint32(36 + dataSize)
	if err := writeWAVHeader(w, riffSize, sampleRate, uint16(numCh), uint16(bitsPerSample), uint32(dataSize)); err != nil {
		return err
	}

	// Write interleaved float32 samples.
	buf := make([]byte, blockAlign)
	for _, vals := range frames {
		for i, v := range vals {
			sample := float32(v / scales[i])
			// Clamp to [-1, 1].
			if sample > 1 {
				sample = 1
			} else if sample < -1 {
				sample = -1
			}
			binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(sample))
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}

	return nil
}

func writeWAVHeader(w io.Writer, riffSize, sampleRate uint32, numChannels, bitsPerSample uint16, dataSize uint32) error {
	blockAlign := numChannels * (bitsPerSample / 8)
	byteRate := sampleRate * uint32(blockAlign)

	// RIFF header
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], riffSize)
	copy(header[8:12], "WAVE")

	// fmt sub-chunk
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // sub-chunk size
	binary.LittleEndian.PutUint16(header[20:22], 3)  // format: IEEE float
	binary.LittleEndian.PutUint16(header[22:24], numChannels)
	binary.LittleEndian.PutUint32(header[24:28], sampleRate)
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], bitsPerSample)

	// data sub-chunk
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	_, err := w.Write(header)
	return err
}
