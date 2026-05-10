package capture

import (
	"encoding/binary"
	"io"
	"math"
	"time"
)

// FrameReader provides random-access reading of uncompressed capture files.
//
// Unlike [Reader], which reads frames sequentially from any [io.Reader],
// FrameReader can seek to arbitrary frame positions. This requires an
// [io.ReadSeeker] and an uncompressed file.
//
// The returned slices from [FrameReader.ReadFrames] are owned by the
// FrameReader and reused across calls. Copy any data you need to retain.
//
// FrameReader is NOT safe for concurrent use.
type FrameReader struct {
	rs        io.ReadSeeker
	header    Header
	dataStart int64
	frameSize int
	numCh     int
	buf       []byte
	frames    []Frame
	closed    bool
}

// NewFrameReader reads the preamble and header from rs and returns a
// FrameReader positioned for random-access reads.
//
// Returns [ErrCompressedSeek] if the file uses zstd compression.
// Returns the same errors as [NewReader] for invalid files.
func NewFrameReader(rs io.ReadSeeker) (*FrameReader, error) {
	pre, err := readPreamble(rs)
	if err != nil {
		return nil, err
	}

	if pre.flags&flagCompressed != 0 {
		return nil, ErrCompressedSeek
	}

	h, err := readHeaderJSON(rs, pre.headerLen)
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
	dataStart := int64(preambleLen) + int64(pre.headerLen)

	return &FrameReader{
		rs:        rs,
		header:    h,
		dataStart: dataStart,
		frameSize: frameSize,
		numCh:     numCh,
	}, nil
}

// Header returns the file header read during construction.
func (fr *FrameReader) Header() Header {
	return fr.header
}

// FrameCount returns the number of frames in the file, or 0 if unknown.
func (fr *FrameReader) FrameCount() uint64 {
	return fr.header.FrameCount
}

// Duration returns the capture duration, or 0 if FrameCount is unknown.
func (fr *FrameReader) Duration() time.Duration {
	return fr.header.Duration()
}

// ReadFrames reads up to n frames starting at frame index offset.
//
// Returns the frames read and any error. If offset is beyond the end of
// the file, returns nil and nil. If fewer than n frames remain, returns
// the available frames and nil.
//
// The returned slice and all Frame data within it are owned by the
// FrameReader and reused on the next call. Copy any values you need
// to retain.
func (fr *FrameReader) ReadFrames(offset uint64, n int) ([]Frame, error) {
	if fr.closed {
		return nil, ErrReaderClosed
	}
	if n <= 0 {
		return nil, nil
	}

	// Clamp to available frames if frame count is known.
	if fr.header.FrameCount > 0 {
		if offset >= fr.header.FrameCount {
			return nil, nil
		}
		if avail := fr.header.FrameCount - offset; uint64(n) > avail {
			n = int(avail)
		}
	}

	pos := fr.dataStart + int64(offset)*int64(fr.frameSize)
	if _, err := fr.rs.Seek(pos, io.SeekStart); err != nil {
		return nil, err
	}

	fr.ensureBuffers(n)

	totalBytes := n * fr.frameSize
	nn, err := io.ReadFull(fr.rs, fr.buf[:totalBytes])
	if err != nil {
		n = nn / fr.frameSize
		if n == 0 {
			return nil, err
		}
	}

	for i := range n {
		fr.decodeFrame(i)
	}

	return fr.frames[:n], nil
}

// Close marks the FrameReader as closed. Subsequent calls to ReadFrames
// return [ErrReaderClosed]. Close does NOT close the underlying
// [io.ReadSeeker].
func (fr *FrameReader) Close() error {
	if fr.closed {
		return ErrReaderClosed
	}
	fr.closed = true
	return nil
}

func (fr *FrameReader) ensureBuffers(n int) {
	for len(fr.frames) < n {
		f := Frame{header: &fr.header}
		switch fr.header.Format {
		case RawUint32:
			f.raw = make([]uint32, fr.numCh)
			f.floats = make([]float64, fr.numCh)
		case calibratedFloat64:
			f.floats = make([]float64, fr.numCh)
		}
		fr.frames = append(fr.frames, f)
	}

	needed := n * fr.frameSize
	if len(fr.buf) < needed {
		fr.buf = make([]byte, needed)
	}
}

func (fr *FrameReader) decodeFrame(i int) {
	off := i * fr.frameSize
	switch fr.header.Format {
	case RawUint32:
		for ch := range fr.numCh {
			fr.frames[i].raw[ch] = binary.LittleEndian.Uint32(fr.buf[off+ch*4:])
		}
	case calibratedFloat64:
		for ch := range fr.numCh {
			fr.frames[i].floats[ch] = math.Float64frombits(binary.LittleEndian.Uint64(fr.buf[off+ch*8:]))
		}
	}
}
