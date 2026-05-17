package device

import "time"

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
	statusAInScanRunning  = 0x0002
	statusAInScanOverrun  = 0x0004
	statusAOutScanRunning = 0x0008
	statusAOutScanUnder   = 0x0010
	statusAInScanDone     = 0x0020
	statusAOutScanDone    = 0x0040
	statusFPGAConfigured  = 0x0100
	statusFPGAConfigMode  = 0x0200
)

// USB endpoint addresses.
const (
	epBulkIn  = 0x86 // EP6 IN - analog input scan data
	epBulkOut = 0x02 // EP2 OUT - analog output scan data
)

// Memory addresses.
const (
	memADCCalBase   = 0x7000
	memDACCalBase   = 0x7100
	memCalDate      = 0x7110
	memSerialNumber = 0x6FF8
)

// USB transfer timeout.
const usbTimeout = 2000 * time.Millisecond

// FPGA configuration unlock code.
const fpgaUnlockCode = 0xAD

// maxTransferBytes is the maximum bytes per USB bulk read.
const maxTransferBytes = 64 * 1024

// ringTransferCount is the number of async bulk transfers in the ring.
const ringTransferCount = 32

// ringMaxStageSize is the maximum buffer size per async ring transfer.
const ringMaxStageSize = 16 * 1024
