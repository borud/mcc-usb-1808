// Package capture provides types and encoding for segmented DAQ capture data.
//
// A capture is stored as a directory of segment files. Each segment is a
// self-contained binary file with a preamble, JSON header, and fixed-width
// frames of sample data. The directory structure looks like:
//
//	capture_20260513_105712/
//	  seg_0000.daq
//	  seg_0001.daq
//	  seg_0002.daq
//
// New captures use [RawUint32] (4 bytes per sample, raw 18-bit ADC / counter /
// digital values). The legacy [calibratedFloat64] format (8 bytes per sample) is
// supported for reading older files but rejected by [NewWriter].
//
// # Segment file layout
//
//	Offset  Len   Field
//	0       4     Magic: "DAQ\x00"
//	4       1     Version (0x02)
//	5       1     Flags (reserved, must be 0)
//	6       4     Header length N (uint32 LE)
//	10      8     Frame count (uint64 LE, 0 = unknown)
//	18      2     Sequence number (uint16 LE)
//	20      8     Global frame offset (uint64 LE)
//	28      N     Header (JSON, UTF-8)
//	28+N    ...   Frame data
//
// The frame count at offset 10 is written as 0 initially and updated by the
// segment writer at close time via seeking. A value of 0 means the count is
// unknown.
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
	fileMagic            = "DAQ\x00"
	fileVersion          = 2
	preambleLen          = 28 // magic(4) + version(1) + flags(1) + headerLen(4) + frameCount(8) + seqNumber(2) + globalFrameOffset(8)
	frameCountOff        = 10 // byte offset of the frame count field in the preamble
	seqNumberOff         = 18 // byte offset of the sequence number
	globalFrameOffsetOff = 20 // byte offset of the global frame offset
)
