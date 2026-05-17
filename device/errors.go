package device

import "errors"

// Sentinel errors returned by Device methods.
var (
	ErrDeviceNotFound    = errors.New("usb1808: device not found")
	ErrFPGANotConfigured = errors.New("usb1808: FPGA not configured")
	ErrScanOverrun       = errors.New("usb1808: analog input scan FIFO overrun")
	ErrScanRunning       = errors.New("usb1808: scan already running")
	ErrInvalidChannel    = errors.New("usb1808: invalid channel number")
	ErrInvalidRange      = errors.New("usb1808: invalid voltage range")
	ErrInvalidMode       = errors.New("usb1808: invalid input mode")
	ErrNotInitialized    = errors.New("usb1808: device not initialized")
)
