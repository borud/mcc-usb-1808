// Package device provides the USB-1808/1808X device driver.
package device

import "math"

// USB vendor and product IDs.
const (
	// VendorID is the USB vendor ID for Measurement Computing Corporation.
	VendorID = 0x09DB

	// PID1808 is the USB product ID for the USB-1808.
	PID1808 = 0x013D
	// PID1808X is the USB product ID for the USB-1808X.
	PID1808X = 0x013E
)

// Model identifies the hardware variant.
type Model uint16

// Hardware model variants.
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

// Analog input voltage ranges.
const (
	// +/- 10V bipolar
	BP10V Range = 0
	// +/- 5V bipolar
	BP5V Range = 1
	// 0-10V unipolar
	UP10V Range = 2
	// 0-5V unipolar
	UP5V Range = 3
)

// 18-bit ADC constants.
const (
	// ADCMidscale is the bipolar zero-crossing point of the 18-bit ADC.
	ADCMidscale = 1 << 17

	// ADCMaxCode is the full-scale code of the 18-bit ADC.
	ADCMaxCode = 1<<18 - 1
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

// Analog input modes.
const (
	Differential InputMode = 0
	SingleEnded  InputMode = 1
	// Grounded input mode. Note: mode code 2 is undefined and rejected.
	Grounded InputMode = 3
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

// Scan queue channel types.
const (
	ChannelTypeAnalog  ChannelType = 0
	ChannelTypeDIO     ChannelType = 1
	ChannelTypeCounter ChannelType = 2
	ChannelTypeEncoder ChannelType = 3
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
		calibrated = max(0, min(ADCMaxCode, calibrated))
	}
	calibrated = math.Round(calibrated)

	switch r {
	case BP10V:
		return (calibrated - ADCMidscale) * 10.0 / ADCMidscale
	case BP5V:
		return (calibrated - ADCMidscale) * 5.0 / ADCMidscale
	case UP10V:
		return calibrated * 10.0 / ADCMaxCode
	case UP5V:
		return calibrated * 5.0 / ADCMaxCode
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
	// NumCounters is the total number of counter/encoder channels (2 counters + 2 encoders).
	NumCounters = 4
	MaxAInQueue     = 13
	MaxAOutQueue    = 3
	MaxPacketSize   = 512
	// BaseClock is the device master clock frequency in Hz (100 MHz).
	BaseClock = 100_000_000
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
	// TriggerEdge selects edge-triggered mode (bit 0: 0=level, 1=edge).
	TriggerEdge = 0x01
	// TriggerHigh selects the active edge/level (bit 1: 0=low/falling, 1=high/rising).
	TriggerHigh = 0x02
)
