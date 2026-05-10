package capture

// DataFormat specifies how sample values are stored in frames.
type DataFormat uint8

const (
	// RawUint32 stores raw device values as uint32 (4 bytes per sample,
	// little-endian). Analog inputs contain 18-bit ADC codes; counters and
	// digital ports contain their native uint32 values. Calibration
	// coefficients in the header allow converting analog values to voltages
	// at read time via [Frame.Values].
	RawUint32 DataFormat = iota

	// calibratedFloat64 is a legacy format (8 bytes per sample). It is
	// accepted by the reader for backward compatibility but cannot be
	// written by new code.
	calibratedFloat64
)
