package capture

import (
	"encoding/binary"
	"io"
	"iter"
	"math"

	"github.com/klauspost/compress/zstd"
)

// Reader reads capture data written by a [Writer].
//
// Frames are read one at a time via [Reader.ReadFrame] or iterated with
// [Reader.Frames]. Internal buffers are reused across calls for zero
// per-frame allocation; callers must copy data they need to retain.
//
// Reader is NOT safe for concurrent use.
type Reader struct {
	r         io.Reader     // data source (raw or zstd decoder)
	dec       *zstd.Decoder // non-nil when compressed
	header    Header
	frameSize int
	numCh     int
	buf       []byte // read buffer, len == frameSize
	frame     Frame  // reused across ReadFrame calls
	closed    bool
}

// NewReader reads the file preamble and header from r and returns a Reader
// positioned at the first frame.
//
// Returns [ErrInvalidMagic] if the magic bytes are wrong.
// Returns [ErrUnsupportedVersion] if the version is not supported.
// Returns [ErrNoChannels] if the header has no channels.
// Returns [ErrInvalidFormat] if the data format is not recognized.
func NewReader(r io.Reader) (*Reader, error) {
	pre, err := readPreamble(r)
	if err != nil {
		return nil, err
	}

	h, err := readHeaderJSON(r, pre.headerLen)
	if err != nil {
		return nil, err
	}
	h.FrameCount = pre.frameCount

	if len(h.Channels) == 0 {
		return nil, ErrNoChannels
	}

	ss, err := sampleSize(h.Format)
	if err != nil {
		return nil, err
	}

	numCh := len(h.Channels)
	frameSize := numCh * ss

	// Set up optional decompression.
	dataReader := r
	var dec *zstd.Decoder
	if pre.flags&flagCompressed != 0 {
		dec, err = zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		dataReader = dec
	}

	frame := Frame{header: &h}
	switch h.Format {
	case RawUint32:
		frame.raw = make([]uint32, numCh)
		frame.floats = make([]float64, numCh)
	case CalibratedFloat64:
		frame.floats = make([]float64, numCh)
	}

	return &Reader{
		r:         dataReader,
		dec:       dec,
		header:    h,
		frameSize: frameSize,
		numCh:     numCh,
		buf:       make([]byte, frameSize),
		frame:     frame,
	}, nil
}

// Header returns the file header read during construction.
func (rd *Reader) Header() Header {
	return rd.header
}

// ReadFrame reads the next frame from the capture file.
//
// The returned [Frame] and its slices are valid until the next call to
// ReadFrame. Returns [io.EOF] when no more frames are available.
// Returns [ErrReaderClosed] if the reader has been closed.
//
// ReadFrame does not allocate after the Reader is constructed.
func (rd *Reader) ReadFrame() (*Frame, error) {
	if rd.closed {
		return nil, ErrReaderClosed
	}

	_, err := io.ReadFull(rd.r, rd.buf)
	if err != nil {
		return nil, err
	}

	switch rd.header.Format {
	case RawUint32:
		for i := range rd.numCh {
			rd.frame.raw[i] = binary.LittleEndian.Uint32(rd.buf[i*4:])
		}
	case CalibratedFloat64:
		for i := range rd.numCh {
			rd.frame.floats[i] = math.Float64frombits(binary.LittleEndian.Uint64(rd.buf[i*8:]))
		}
	}

	return &rd.frame, nil
}

// Frames returns an iterator over all remaining frames. Iteration stops
// at [io.EOF]. Any other error is yielded to the caller and iteration stops.
func (rd *Reader) Frames() iter.Seq2[*Frame, error] {
	return func(yield func(*Frame, error) bool) {
		for {
			frame, err := rd.ReadFrame()
			if err == io.EOF {
				return
			}
			if !yield(frame, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// Close releases resources held by the Reader (e.g. the zstd decoder).
// After Close, all subsequent calls to [ReadFrame] return [ErrReaderClosed].
//
// Close does NOT close the underlying [io.Reader].
func (rd *Reader) Close() error {
	if rd.closed {
		return ErrReaderClosed
	}
	rd.closed = true
	if rd.dec != nil {
		rd.dec.Close()
	}
	return nil
}
