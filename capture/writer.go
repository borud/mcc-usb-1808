package capture

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultSegmentSize = 100 * 1024 * 1024 // 100 MB
)

// WriterOption configures a [Writer].
type WriterOption func(*writerConfig)

type writerConfig struct {
	bufferFrames int
	segmentSize  int
}

// WithBufferSize sets the number of frames to buffer before flushing to the
// underlying writer. The default is 1024 frames.
func WithBufferSize(frames int) WriterOption {
	return func(c *writerConfig) {
		if frames > 0 {
			c.bufferFrames = frames
		}
	}
}

// WithFileSize sets the target segment size in bytes. Segments are rotated
// after approximately this many bytes of frame data. The actual split point is
// calculated by frame count, so segments may be slightly larger if a
// [Writer.WriteBulk] call straddles the boundary.
// The default is 100 MB.
func WithFileSize(bytes int) WriterOption {
	return func(c *writerConfig) {
		if bytes > 0 {
			c.segmentSize = bytes
		}
	}
}

// Writer writes segmented capture data to a directory.
//
// Frames are split across segment files of roughly equal size. The next
// segment file is pre-opened so that rotation does not stall the write
// pipeline.
//
// A Writer must be closed after use to finalize the last segment and
// clean up any pre-opened pending files.
//
// Writer is NOT safe for concurrent use.
type Writer struct {
	dir         string
	header      Header
	frameSize   int
	maxFrames   uint64
	bufFrames   int
	current     *segmentWriter
	next        *segmentWriter
	seq         uint16
	totalFrames uint64
	closed      bool
}

// NewWriter creates a Writer that writes segmented capture data to dir.
//
// The directory is created if it does not exist. The first segment file
// is opened immediately, and a second segment is pre-opened for smooth
// rotation.
//
// Returns [ErrNoChannels] if h.Channels is empty.
// Returns [ErrInvalidFormat] if h.Format is not [RawUint32].
func NewWriter(dir string, h Header, opts ...WriterOption) (*Writer, error) {
	if len(h.Channels) == 0 {
		return nil, ErrNoChannels
	}
	if h.Format != RawUint32 {
		return nil, ErrInvalidFormat
	}

	cfg := writerConfig{
		bufferFrames: defaultBufferFrames,
		segmentSize:  defaultSegmentSize,
	}
	for _, o := range opts {
		o(&cfg)
	}

	const sampleBytes = 4
	numCh := len(h.Channels)
	frameSize := numCh * sampleBytes
	maxFrames := uint64(cfg.segmentSize / frameSize)
	if maxFrames == 0 {
		maxFrames = 1
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}

	w := &Writer{
		dir:       dir,
		header:    h,
		frameSize: frameSize,
		maxFrames: maxFrames,
		bufFrames: cfg.bufferFrames,
	}

	current, err := w.openSegment(0, 0)
	if err != nil {
		return nil, err
	}
	w.current = current
	w.seq = 1

	next, err := w.openSegment(1, maxFrames)
	if err != nil {
		current.closeRemoveIfEmpty()
		return nil, err
	}
	w.next = next
	w.seq = 2

	return w, nil
}

func (w *Writer) openSegment(seq uint16, globalOffset uint64) (*segmentWriter, error) {
	name := fmt.Sprintf("seg_%04d.daq", seq)
	finalPath := filepath.Join(w.dir, name)
	pendingPath := finalPath + ".pending"
	return newSegmentWriter(pendingPath, finalPath, w.header, seq, globalOffset, w.bufFrames)
}

// WriteBulk writes pre-formatted frame data directly. The caller must
// ensure data contains correctly formatted little-endian uint32 samples
// (numCh samples per frame). This is the zero-copy fast path for raw
// captures where USB bulk data is already in the target wire format.
//
// The current segment may slightly exceed the target size to avoid
// splitting a bulk write. Rotation happens after the write completes.
//
// Returns [ErrWriterClosed] if the writer has been closed.
func (w *Writer) WriteBulk(data []byte) error {
	if w.closed {
		return ErrWriterClosed
	}

	frames := uint64(len(data) / w.frameSize)
	if err := w.current.writeBulk(data); err != nil {
		return err
	}
	w.totalFrames += frames

	if w.current.framesWritten >= w.maxFrames {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	return nil
}

// Flush writes any buffered frame data in the current segment.
// Returns [ErrWriterClosed] if the writer has been closed.
func (w *Writer) Flush() error {
	if w.closed {
		return ErrWriterClosed
	}
	return w.current.flush()
}

// Close finalizes the current segment, removes any unused pre-opened
// segment, and marks the writer as closed. After Close, all subsequent
// calls return [ErrWriterClosed].
func (w *Writer) Close() error {
	if w.closed {
		return ErrWriterClosed
	}
	w.closed = true

	err := w.current.closeKeep()
	w.next.closeRemoveIfEmpty()
	return err
}

// FramesWritten returns the total number of frames written across all
// segments. This is valid both during writing and after [Writer.Close].
func (w *Writer) FramesWritten() uint64 {
	return w.totalFrames
}

func (w *Writer) rotate() error {
	if err := w.current.closeKeep(); err != nil {
		return err
	}

	w.current = w.next
	// Patch the globalOffset to the actual total at rotation time,
	// since the previous segment may have overshot maxFrames.
	if err := w.current.patchGlobalOffset(w.totalFrames); err != nil {
		return err
	}

	next, err := w.openSegment(w.seq, w.totalFrames+w.maxFrames)
	if err != nil {
		return err
	}
	w.next = next
	w.seq++
	return nil
}
