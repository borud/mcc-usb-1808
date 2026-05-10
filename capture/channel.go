package capture

// ChannelType identifies the kind of data source.
type ChannelType uint8

// ChannelType values.
const (
	AnalogIn  ChannelType = iota // Analog input (channels 0-7)
	DigitalIO                    // Digital I/O port (channel 8)
	Counter                      // Counter (channels 9-10)
	Encoder                      // Quadrature encoder (channels 11-12)
)

// CalEntry holds calibration coefficients for a single channel at its
// configured voltage range. These are the slope and offset values read
// from the device EEPROM.
type CalEntry struct {
	Slope  float32 `json:"slope"`
	Offset float32 `json:"offset"`
}

// Channel describes a single channel in the capture.
type Channel struct {
	Index int         `json:"index"`            // Hardware channel index.
	Type  ChannelType `json:"type"`             // Channel type.
	Range uint8       `json:"range,omitempty"`  // Voltage range code (0=BP10V, 1=BP5V, 2=UP10V, 3=UP5V). AnalogIn only.
	Name  string      `json:"name,omitempty"`   // Optional human-readable label.
	Cal   *CalEntry   `json:"cal,omitempty"`    // Calibration coefficients. AnalogIn + RawUint32 only.
}
