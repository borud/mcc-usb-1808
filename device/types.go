// Package device provides the USB-1808/1808X device driver.
package device

import "math"

// USB vendor and product IDs.
const (
	VendorID = 0x09DB // Measurement Computing Corporation

	PID1808  = 0x013D // USB-1808
	PID1808X = 0x013E // USB-1808X
)

// Model identifies the hardware variant.
type Model uint16

const (
	USB1808  Model = PID1808
	USB1808X Model = PID1808X
)

func (m Model) String() string {
	switch m {
	case USB1808:
		return "USB-1808"
	case USB1808X:
		return "USB-1808X"
	default:
		return "unknown"
	}
}

// Range codes for analog input channels.
type Range uint8

const (
	BP10V Range = 0 // +/- 10V bipolar
	BP5V  Range = 1 // +/- 5V bipolar
	UP10V Range = 2 // 0-10V unipolar
	UP5V  Range = 3 // 0-5V unipolar
)

func (r Range) String() string {
	switch r {
	case BP10V:
		return "±10V"
	case BP5V:
		return "±5V"
	case UP10V:
		return "0-10V"
	case UP5V:
		return "0-5V"
	default:
		return "unknown"
	}
}

// InputMode codes for analog input channels.
type InputMode uint8

const (
	Differential InputMode = 0
	SingleEnded  InputMode = 1
	Grounded     InputMode = 3 // note: 2 is undefined and rejected
)

func (m InputMode) String() string {
	switch m {
	case Differential:
		return "differential"
	case SingleEnded:
		return "single-ended"
	case Grounded:
		return "grounded"
	default:
		return "unknown"
	}
}

// ChannelType identifies the kind of channel in the scan queue.
type ChannelType uint8

const (
	ChannelTypeAnalog   ChannelType = 0
	ChannelTypeDIO      ChannelType = 1
	ChannelTypeCounter  ChannelType = 2
	ChannelTypeEncoder  ChannelType = 3
)

func (t ChannelType) String() string {
	switch t {
	case ChannelTypeAnalog:
		return "analog"
	case ChannelTypeDIO:
		return "dio"
	case ChannelTypeCounter:
		return "counter"
	case ChannelTypeEncoder:
		return "encoder"
	default:
		return "unknown"
	}
}

// ChannelConfig describes a single channel in the scan queue.
type ChannelConfig struct {
	Index int
	Type  ChannelType
	Range Range
	Mode  InputMode
}

// Calibration holds a single calibration coefficient pair read from EEPROM.
type Calibration struct {
	Slope  float32
	Offset float32
}

// ToVolts converts a raw 18-bit ADC value to voltage using these calibration
// coefficients and the given voltage range.
func (c Calibration) ToVolts(raw uint32, r Range) float64 {
	raw18 := raw & 0x3FFFF
	calibrated := float64(raw18)*float64(c.Slope) + float64(c.Offset)

	// Clamp for unipolar ranges.
	if r >= UP10V {
		calibrated = max(0, min(262143, calibrated))
	}
	calibrated = math.Round(calibrated)

	switch r {
	case BP10V:
		return (calibrated - 131072.0) * 10.0 / 131072.0
	case BP5V:
		return (calibrated - 131072.0) * 5.0 / 131072.0
	case UP10V:
		return calibrated * 10.0 / 262143.0
	case UP5V:
		return calibrated * 5.0 / 262143.0
	default:
		return float64(raw)
	}
}

// CalibrationTable holds the full set of analog input calibration coefficients.
type CalibrationTable [NumAInChannels][NumAInRanges]Calibration

// Device limits.
const (
	NumAInChannels  = 8
	NumAInRanges    = 4
	NumAOutChannels = 2
	NumTimers       = 2
	NumCounters     = 4 // 2 counters + 2 encoders
	MaxAInQueue     = 13
	MaxAOutQueue    = 3
	MaxPacketSize   = 512
	BaseClock       = 100_000_000 // 100 MHz
)

// Scan queue channel selectors.
const (
	ScanChanAIn0     = 0
	ScanChanAIn1     = 1
	ScanChanAIn2     = 2
	ScanChanAIn3     = 3
	ScanChanAIn4     = 4
	ScanChanAIn5     = 5
	ScanChanAIn6     = 6
	ScanChanAIn7     = 7
	ScanChanDIO      = 8
	ScanChanCounter0 = 9
	ScanChanCounter1 = 10
	ScanChanEncoder0 = 11
	ScanChanEncoder1 = 12
)

// Counter and timer indices.
const (
	Counter0 = 0
	Counter1 = 1
	Encoder0 = 2
	Encoder1 = 3
	Timer0   = 0
	Timer1   = 1
)

// Analog input scan options.
const (
	ScanOptExternalTrigger  = 0x01
	ScanOptPatternDetection = 0x02
	ScanOptRetriggerMode    = 0x04
	ScanOptCounterValue     = 0x08
	ScanOptSingleIO         = 0x10
)

// Trigger configuration bits.
const (
	TriggerEdge = 0x01 // bit 0: 0=level, 1=edge
	TriggerHigh = 0x02 // bit 1: 0=low/falling, 1=high/rising
)
