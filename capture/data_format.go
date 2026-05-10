package capture

// DataFormat specifies how sample values are stored in frames.
type DataFormat uint8

const (
	// RawUint32 stores raw device values as uint32 (4 bytes per sample).
	// Analog inputs contain 18-bit ADC codes; counters and digital ports
	// contain their native uint32 values. Calibration coefficients in the
	// header allow converting analog values to voltages via [Frame.Values].
	RawUint32 DataFormat = iota

	// CalibratedFloat64 stores pre-calibrated float64 values (8 bytes per
	// sample). Analog inputs are voltages; other channel types are cast
	// from their native integer representation.
	CalibratedFloat64
)
