package usb1808

import "time"

// USB vendor and product IDs.
const (
	VendorID = 0x09DB // Measurement Computing Corporation

	PID1808  = 0x013D // USB-1808
	PID1808X = 0x013E // USB-1808X
)

// Model identifies the hardware variant.
type Model uint16

// Model constants for USB-1808 variants.
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

// Command codes (bRequest values for USB control transfers).
const (
	cmdDTristate     = 0x00
	cmdDPort         = 0x01
	cmdDLatch        = 0x02
	cmdAIn           = 0x10
	cmdADCSetup      = 0x11
	cmdAInScanStart  = 0x12
	cmdAInScanStop   = 0x13
	cmdAInConfig     = 0x14
	cmdAInClearFIFO  = 0x15
	cmdAInBulkFlush  = 0x16
	cmdAOut          = 0x18
	cmdAOutScanConf  = 0x19
	cmdAOutScanStart = 0x1A
	cmdAOutScanStop  = 0x1B
	cmdAOutClearFIFO = 0x1C
	cmdCounter       = 0x20
	cmdCounterOpts   = 0x21
	cmdCounterLimits = 0x22
	cmdCounterMode   = 0x23
	cmdCounterParam  = 0x24
	cmdTimerControl  = 0x28
	cmdTimerParams   = 0x2D
	cmdMemory        = 0x30
	cmdMemAddress    = 0x31
	cmdMemWriteEn    = 0x32
	cmdStatus        = 0x40
	cmdBlinkLED      = 0x41
	cmdReset         = 0x42
	cmdTriggerConfig = 0x43
	cmdPatternDetect = 0x44
	cmdSerial        = 0x48
	cmdFPGAConfig    = 0x50
	cmdFPGAData      = 0x51
	cmdFPGAVersion   = 0x52
)

// Status bits returned by the STATUS command.
const (
	StatusAInScanRunning  = 0x0002 // Analog input pacer running
	StatusAInScanOverrun  = 0x0004 // Analog input scan FIFO overrun
	StatusAOutScanRunning = 0x0008 // Analog output scan running
	StatusAOutScanUnder   = 0x0010 // Analog output scan FIFO underrun
	StatusAInScanDone     = 0x0020 // Analog input scan completed
	StatusAOutScanDone    = 0x0040 // Analog output scan completed
	StatusFPGAConfigured  = 0x0100 // FPGA is configured and ready
	StatusFPGAConfigMode  = 0x0200 // Device is in FPGA configuration mode
)

// Range codes for analog input channels.
type Range uint8

// Analog input range codes.
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

// Analog input mode codes.
const (
	Differential InputMode = 0
	SingleEnded  InputMode = 1
	Grounded     InputMode = 3 // note: 2 is undefined and rejected
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

// Counter mode bits.
const (
	CounterTotalize    = 0x00
	CounterPeriod      = 0x01
	CounterPulseWidth  = 0x02
	CounterTiming      = 0x03
	PeriodMode1X       = 0x00
	PeriodMode10X      = 0x04
	PeriodMode100X     = 0x08
	PeriodMode1000X    = 0x0C
	TickSize20NS       = 0x00
	TickSize200NS      = 0x10
	TickSize2000NS     = 0x20
	TickSize20000NS    = 0x30
)

// Counter options bits (for counters 0-1).
const (
	CounterClearOnRead = 0x01
	CounterNoRecycle   = 0x02
	CounterCountDown   = 0x04
	CounterRangeLimit  = 0x08
	CounterFallingEdge = 0x10
)

// Encoder options bits (for encoders 2-3).
const (
	EncoderX1         = 0x00
	EncoderX2         = 0x01
	EncoderX4         = 0x02
	EncoderClearOnZ   = 0x04
	EncoderLatchOnZ   = 0x08
	EncoderNoRecycle  = 0x10
	EncoderRangeLimit = 0x20
)

// Timer control bits.
const (
	TimerEnable     = 0x01
	TimerRunning    = 0x02
	TimerInverted   = 0x04
	TimerOTrigBegin = 0x10
	TimerOTrig      = 0x40
)

// Analog input scan options.
const (
	ScanOptExternalTrigger  = 0x01
	ScanOptPatternDetection = 0x02
	ScanOptRetriggerMode    = 0x04
	ScanOptCounterValue     = 0x08 // maintain counter value on scan start
	ScanOptSingleIO         = 0x10
)

// Analog output scan options.
const (
	AOutOptTrigger   = 0x10
	AOutOptRetrigger = 0x20
)

// Scan queue channel selectors for AIn scan.
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

// Scan queue channel selectors for AOut scan.
const (
	AOutScanChanAOut0 = 0
	AOutScanChanAOut1 = 1
	AOutScanChanDIO   = 2
)

// Trigger configuration bits.
const (
	TriggerEdge = 0x01 // bit 0: 0=level, 1=edge
	TriggerHigh = 0x02 // bit 1: 0=low/falling, 1=high/rising
)

// Pattern detection comparison modes (bits 1-2 of options byte).
const (
	PatternEqual      = 0x00
	PatternNotEqual   = 0x02
	PatternGreaterThn = 0x04
	PatternLessThan   = 0x06
)

// USB endpoint addresses.
const (
	epBulkIn  = 0x86 // EP6 IN - analog input scan data
	epBulkOut = 0x02 // EP2 OUT - analog output scan data
)

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

// Memory addresses.
const (
	memADCCalBase   = 0x7000
	memDACCalBase   = 0x7100
	memCalDate      = 0x7110
	memSerialNumber = 0x6FF8
	memCalUnlock    = 0x8000
	memCalUnlockVal = 0xAA55
)

// USB transfer timeout.
const usbTimeout = 2000 * time.Millisecond

// FPGA configuration unlock code.
const fpgaUnlockCode = 0xAD
