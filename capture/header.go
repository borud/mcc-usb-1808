package capture

import (
	"time"
)

// Header is the capture file metadata, written once at the start of the file.
type Header struct {
	// Device identification.
	DeviceModel     string    `json:"device_model"`
	DeviceSerial    string    `json:"device_serial"`
	FPGAVersion     string    `json:"fpga_version"`               // e.g. "1.2"
	CalibrationDate time.Time `json:"calibration_date,omitzero"` // Factory calibration date from EEPROM.

	// Capture configuration.
	Channels   []Channel  `json:"channels"`
	SampleRate float64    `json:"sample_rate"` // Hz per channel.
	Format     DataFormat `json:"format"`
	FrameCount uint64     `json:"-"` // Stored in the binary preamble, not in JSON. 0 = unknown.

	// Optional session metadata.
	ApplicationName string            `json:"application_name,omitempty"` // Name of the application that captured the data.
	SessionID       string            `json:"session_id,omitempty"`       // Identifier for this capture session.
	Description     string            `json:"description,omitempty"`      // Free-form description.
	Operator        string            `json:"operator,omitempty"`         // Person or system running the capture.
	Timestamp       int64             `json:"timestamp,omitempty"`        // Capture start as milliseconds since Unix epoch.
	Properties      map[string]string `json:"properties,omitempty"`       // Arbitrary key-value metadata.
}

// FrameAtTime returns the frame index corresponding to the given time offset
// in seconds from the start of the capture. Negative offsets return 0.
func (h *Header) FrameAtTime(seconds float64) uint64 {
	if seconds <= 0 || h.SampleRate <= 0 {
		return 0
	}
	return uint64(seconds * h.SampleRate)
}

// TimeAtFrame returns the time offset in seconds from the start of the
// capture for the given frame index.
func (h *Header) TimeAtFrame(index uint64) float64 {
	if h.SampleRate <= 0 {
		return 0
	}
	return float64(index) / h.SampleRate
}

// Duration returns the capture duration based on FrameCount and SampleRate.
// Returns 0 if FrameCount is unknown (0) or SampleRate is non-positive.
func (h *Header) Duration() time.Duration {
	if h.FrameCount == 0 || h.SampleRate <= 0 {
		return 0
	}
	return time.Duration(float64(h.FrameCount) / h.SampleRate * float64(time.Second))
}
