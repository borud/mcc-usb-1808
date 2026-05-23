package capture

import (
	"fmt"
	"time"

	"github.com/borud/mcc-usb-1808/v4/device"
)

// scanChanNames maps hardware channel indices to human-readable names.
var scanChanNames = map[int]string{
	0: "ain0", 1: "ain1", 2: "ain2", 3: "ain3",
	4: "ain4", 5: "ain5", 6: "ain6", 7: "ain7",
	8: "dio",
	9: "counter0", 10: "counter1",
	11: "encoder0", 12: "encoder1",
}

// NewHeader builds a capture Header from a device and scan configuration.
// It queries the device for serial number, FPGA version, calibration date,
// and calibration coefficients. Timestamp is set to the current time.
func NewHeader(dev *device.Device, cfg device.ScanConfig) (Header, error) {
	serial, err := dev.SerialNumber()
	if err != nil {
		return Header{}, fmt.Errorf("serial: %w", err)
	}

	major, minor, err := dev.FPGAVersion()
	if err != nil {
		return Header{}, fmt.Errorf("fpga version: %w", err)
	}

	calDate, _ := dev.CalibrationDate()
	calTable := dev.CalibrationTable()

	channels := make([]Channel, len(cfg.Channels))
	for i, ch := range cfg.Channels {
		cc := Channel{
			Index: ch.Index,
			Type:  ch.Type,
			Name:  scanChanNames[ch.Index],
		}
		if ch.Type == device.ChannelTypeAnalog {
			cc.Range = ch.Range
			cal := calTable[ch.Index][ch.Range]
			cc.Cal = &cal
		}
		channels[i] = cc
	}

	return Header{
		DeviceModel:     dev.Model().String(),
		DeviceSerial:    serial,
		FPGAVersion:     fmt.Sprintf("%d.%d", major, minor),
		CalibrationDate: calDate,
		Channels:        channels,
		SampleRate:      float64(cfg.Rate),
		Format:          RawUint32,
		Timestamp:       time.Now().UnixMilli(),
	}, nil
}

// Header is the capture file metadata, written once at the start of the file.
type Header struct {
	// Device identification.
	DeviceModel     string    `json:"device_model"`
	DeviceSerial    string    `json:"device_serial"`
	// FPGAVersion is the FPGA firmware version string (e.g. "1.2").
	FPGAVersion string `json:"fpga_version"`
	// CalibrationDate is the factory calibration date from EEPROM.
	CalibrationDate time.Time `json:"calibration_date,omitzero"`

	// Capture configuration.
	Channels []Channel `json:"channels"`
	// SampleRate is the sample rate in Hz per channel.
	SampleRate float64 `json:"sample_rate"`
	Format     DataFormat `json:"format"`
	// FrameCount is stored in the binary preamble, not in JSON. 0 = unknown.
	FrameCount uint64 `json:"-"`

	// Optional session metadata.

	// ApplicationName is the name of the application that captured the data.
	ApplicationName string `json:"application_name,omitempty"`
	// SessionID is the identifier for this capture session.
	SessionID string `json:"session_id,omitempty"`
	// Description is a free-form description.
	Description string `json:"description,omitempty"`
	// Operator is the person or system running the capture.
	Operator string `json:"operator,omitempty"`
	// Timestamp is the capture start as milliseconds since Unix epoch.
	Timestamp int64 `json:"timestamp,omitempty"`
	// Properties holds arbitrary key-value metadata.
	Properties map[string]string `json:"properties,omitempty"`
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
