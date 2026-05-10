package capture

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"

	"github.com/klauspost/compress/zstd"
)

const defaultBufferFrames = 1024

// WriterOption configures a [Writer].
type WriterOption func(*writerConfig)

type writerConfig struct {
	bufferFrames int
	compressed   bool
}

// WithBufferSize sets the number of frames to buffer before flushing to the
// underlying writer. The actual byte buffer is bufferFrames * frameSize.
// A larger buffer reduces the number of write syscalls and improves
// throughput, especially for network or compressed output.
// The default is 1024 frames.
func WithBufferSize(frames int) WriterOption {
	return func(c *writerConfig) {
		if frames > 0 {
			c.bufferFrames = frames
		}
	}
}

// WithCompression enables zstd compression of frame data. The file header
// is always uncompressed. Compression adds CPU overhead but can
// significantly reduce file size for slowly-changing signals.
func WithCompression(enabled bool) WriterOption {
	return func(c *writerConfig) {
		c.compressed = enabled
	}
}

// Writer writes capture data to an [io.Writer].
//
// Frames are buffered internally and flushed when the buffer is full or
// when [Writer.Flush] or [Writer.Close] is called. The buffer size is a
// configurable multiple of the frame size (see [WithBufferSize]).
//
// A Writer must be closed after use to flush remaining data and finalize
// any compression stream.
//
// Writer is NOT safe for concurrent use.
type Writer struct {
	w         io.Writer     // data destination (raw or zstd encoder)
	raw       io.Writer     // original writer (for seeking back to patch frame count)
	enc       *zstd.Encoder // non-nil when compressed
	numCh         int
	frameSize     int // bytes per frame
	format        DataFormat
	buf           []byte
	bufUsed       int
	framesWritten uint64
	closed        bool
}

// NewWriter creates a Writer that writes capture data to w.
//
// The file preamble and JSON header are written immediately. Frame data
// follows, optionally zstd-compressed.
//
// Returns [ErrNoChannels] if h.Channels is empty.
// Returns [ErrInvalidFormat] if h.Format is not recognized.
func NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error) {
	if len(h.Channels) == 0 {
		return nil, ErrNoChannels
	}

	cfg := writerConfig{bufferFrames: defaultBufferFrames}
	for _, o := range opts {
		o(&cfg)
	}

	var sampleSize int
	switch h.Format {
	case RawUint32:
		sampleSize = 4
	case CalibratedFloat64:
		sampleSize = 8
	default:
		return nil, ErrInvalidFormat
	}
	numCh := len(h.Channels)
	frameSize := numCh * sampleSize

	// Encode header.
	headerJSON, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}

	// Build and write preamble.
	var flags uint8
	if cfg.compressed {
		flags |= flagCompressed
	}

	preamble := make([]byte, preambleLen)
	copy(preamble, fileMagic)
	preamble[4] = fileVersion
	preamble[5] = flags
	binary.LittleEndian.PutUint32(preamble[6:], uint32(len(headerJSON)))
	// Frame count at offset 10 is left as 0 (unknown); patched by Close.

	if _, err := w.Write(preamble); err != nil {
		return nil, err
	}
	if _, err := w.Write(headerJSON); err != nil {
		return nil, err
	}

	// Set up optional compression.
	dataWriter := w
	var enc *zstd.Encoder
	if cfg.compressed {
		enc, err = zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			return nil, err
		}
		dataWriter = enc
	}

	return &Writer{
		w:         dataWriter,
		raw:       w,
		enc:       enc,
		numCh:     numCh,
		frameSize: frameSize,
		format:    h.Format,
		buf:       make([]byte, cfg.bufferFrames*frameSize),
	}, nil
}

// WriteFrame writes one frame of raw uint32 values.
//
// The writer's format must be [RawUint32]; otherwise [ErrInvalidFormat] is
// returned. Returns [ErrFrameSizeMismatch] if len(values) != len(channels).
// Returns [ErrWriterClosed] if the writer has been closed.
//
// WriteFrame does not allocate after the Writer is constructed.
func (w *Writer) WriteFrame(values []uint32) error {
	if w.closed {
		return ErrWriterClosed
	}
	if w.format != RawUint32 {
		return ErrInvalidFormat
	}
	if len(values) != w.numCh {
		return ErrFrameSizeMismatch
	}

	if w.bufUsed+w.frameSize > len(w.buf) {
		if err := w.Flush(); err != nil {
			return err
		}
	}

	for i, v := range values {
		binary.LittleEndian.PutUint32(w.buf[w.bufUsed+i*4:], v)
	}
	w.bufUsed += w.frameSize
	w.framesWritten++
	return nil
}

// WriteFrameFloat64 writes one frame of calibrated float64 values.
//
// The writer's format must be [CalibratedFloat64]; otherwise
// [ErrInvalidFormat] is returned. Returns [ErrFrameSizeMismatch] if
// len(values) != len(channels). Returns [ErrWriterClosed] if the writer
// has been closed.
//
// WriteFrameFloat64 does not allocate after the Writer is constructed.
func (w *Writer) WriteFrameFloat64(values []float64) error {
	if w.closed {
		return ErrWriterClosed
	}
	if w.format != CalibratedFloat64 {
		return ErrInvalidFormat
	}
	if len(values) != w.numCh {
		return ErrFrameSizeMismatch
	}

	if w.bufUsed+w.frameSize > len(w.buf) {
		if err := w.Flush(); err != nil {
			return err
		}
	}

	for i, v := range values {
		binary.LittleEndian.PutUint64(w.buf[w.bufUsed+i*8:], math.Float64bits(v))
	}
	w.bufUsed += w.frameSize
	w.framesWritten++
	return nil
}

// Flush writes any buffered frame data to the underlying writer.
// Returns [ErrWriterClosed] if the writer has been closed.
func (w *Writer) Flush() error {
	if w.closed {
		return ErrWriterClosed
	}
	if w.bufUsed == 0 {
		return nil
	}
	_, err := w.w.Write(w.buf[:w.bufUsed])
	w.bufUsed = 0
	return err
}

// Close flushes any remaining buffered data and finalizes the compression
// stream (if enabled). After Close, all subsequent calls return
// [ErrWriterClosed].
//
// Close does NOT close the underlying [io.Writer].
func (w *Writer) Close() error {
	if w.closed {
		return ErrWriterClosed
	}
	w.closed = true

	if err := w.flush(); err != nil {
		return err
	}
	if w.enc != nil {
		if err := w.enc.Close(); err != nil {
			return err
		}
	}

	// Patch the frame count in the preamble if the underlying writer
	// supports seeking. Non-seekable writers (pipes, network) keep the
	// initial value of 0 (unknown).
	if ws, ok := w.raw.(io.WriteSeeker); ok {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], w.framesWritten)
		if _, err := ws.Seek(frameCountOff, io.SeekStart); err != nil {
			return err
		}
		if _, err := ws.Write(buf[:]); err != nil {
			return err
		}
	}
	return nil
}

// FramesWritten returns the number of frames successfully written so far.
// This is valid both during writing and after [Writer.Close].
func (w *Writer) FramesWritten() uint64 {
	return w.framesWritten
}

// flush is the internal flush that works even after closed is set (used by Close).
func (w *Writer) flush() error {
	if w.bufUsed == 0 {
		return nil
	}
	_, err := w.w.Write(w.buf[:w.bufUsed])
	w.bufUsed = 0
	return err
}
