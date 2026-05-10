package capture

import "errors"

// Sentinel errors.
var (
	// ErrInvalidMagic is returned by [NewReader] when the file does not
	// start with the expected magic bytes.
	ErrInvalidMagic = errors.New("capture: invalid file magic")

	// ErrUnsupportedVersion is returned by [NewReader] when the file
	// version is newer than this package supports.
	ErrUnsupportedVersion = errors.New("capture: unsupported file version")

	// ErrFrameSizeMismatch is returned by [Writer.WriteFrame] or
	// [Writer.WriteFrameFloat64] when the number of values does not
	// match the number of channels in the header.
	ErrFrameSizeMismatch = errors.New("capture: value count does not match channel count")

	// ErrWriterClosed is returned by any [Writer] method called after
	// [Writer.Close].
	ErrWriterClosed = errors.New("capture: writer is closed")

	// ErrReaderClosed is returned by any [Reader] method called after
	// [Reader.Close].
	ErrReaderClosed = errors.New("capture: reader is closed")

	// ErrNoChannels is returned when a [Header] has an empty Channels
	// slice.
	ErrNoChannels = errors.New("capture: header has no channels")

	// ErrInvalidFormat is returned when a [Header.Format] value is not
	// recognized, or when a write method is called that does not match
	// the writer's configured format.
	ErrInvalidFormat = errors.New("capture: invalid data format for this operation")

	// ErrCompressedSeek is returned by [NewFrameReader] when the file
	// uses zstd compression, which does not support random access.
	ErrCompressedSeek = errors.New("capture: compressed files do not support random access")
)
