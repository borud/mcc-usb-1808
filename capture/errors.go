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

	// ErrNotDirectory is returned by [NewReader] when the path is not
	// a directory.
	ErrNotDirectory = errors.New("capture: path is not a directory")

	// ErrNoSegments is returned by [NewReader] when the directory
	// contains no segment files.
	ErrNoSegments = errors.New("capture: no segment files found")

	// ErrSegmentMismatch is returned by [NewReader] when segment headers
	// are inconsistent across files in the same capture directory.
	ErrSegmentMismatch = errors.New("capture: segment headers are inconsistent")
)
