// Package capture provides types and encoding for DAQ capture data.
//
// A capture file stores sampled data from a USB-1808 DAQ device in a compact
// binary format. The file begins with a short preamble and JSON header
// describing the channels, sample rate, calibration, and data format, followed
// by fixed-width frames of sample data.
//
// New captures use [RawUint32] (4 bytes per sample, raw 18-bit ADC / counter /
// digital values). The legacy [calibratedFloat64] format (8 bytes per sample) is
// supported for reading older files but rejected by [NewWriter].
//
// Frame data may optionally be zstd-compressed.
//
// # File layout
//
//	Offset  Len   Field
//	0       4     Magic: "DAQ\x00"
//	4       1     Version (0x01)
//	5       1     Flags (bit 0 = zstd compressed)
//	6       4     Header length N (uint32 LE)
//	10      8     Frame count (uint64 LE, 0 = unknown)
//	18      N     Header (JSON, UTF-8)
//	18+N    ...   Frame data
//
// The frame count at offset 10 is written as 0 initially and updated by
// [Writer.Close] if the underlying writer supports seeking. A value of 0
// means the count is unknown (e.g. streaming to a pipe).
//
// Each frame is len(Header.Channels) samples, stored consecutively with no
// padding or per-frame delimiters. Frame k starts at byte offset
// headerEnd + k*frameSize.
//
// # Ownership and lifetime
//
// [Reader.ReadFrame] returns a pointer to an internal [Frame] whose slices are
// reused across calls. Callers must copy any values they need to retain past
// the next ReadFrame call. This is the same contract as [bufio.Scanner.Bytes].
//
// [Writer] and [Reader] are NOT safe for concurrent use.
package capture

// File format constants.
const (
	fileMagic       = "DAQ\x00"
	fileVersion     = 1
	preambleLen     = 18 // magic(4) + version(1) + flags(1) + headerLen(4) + frameCount(8)
	frameCountOff   = 10 // byte offset of the frame count field in the preamble
)

// Flag bits stored in the preamble.
const (
	flagCompressed = 0x01
)
