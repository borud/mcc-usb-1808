package usb1808

import "errors"

// Sentinel errors returned by Device methods.
var (
	ErrDeviceNotFound    = errors.New("usb1808: device not found")
	ErrFPGANotConfigured = errors.New("usb1808: FPGA not configured")
	ErrScanOverrun       = errors.New("usb1808: analog input scan FIFO overrun")
	ErrScanUnderrun      = errors.New("usb1808: analog output scan FIFO underrun")
	ErrScanRunning       = errors.New("usb1808: scan already running")
	ErrInvalidChannel    = errors.New("usb1808: invalid channel number")
	ErrInvalidRange      = errors.New("usb1808: invalid voltage range")
	ErrInvalidMode       = errors.New("usb1808: invalid input mode")
	ErrTransferFailed    = errors.New("usb1808: USB transfer failed")
	ErrTimeout           = errors.New("usb1808: USB transfer timeout")
	ErrNotInitialized    = errors.New("usb1808: device not initialized")
	ErrAOutScanRunning   = errors.New("usb1808: cannot write AOut while output scan is running")
	ErrInvalidTimer      = errors.New("usb1808: invalid timer number")
	ErrInvalidCounter    = errors.New("usb1808: invalid counter number")
)
