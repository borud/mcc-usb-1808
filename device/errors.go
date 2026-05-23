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
	ErrScanNotStarted    = errors.New("usb1808: scan not started")
	ErrScanStopped       = errors.New("usb1808: scan already stopped")

	ErrAOutScanRunning    = errors.New("usb1808: analog output scan already running")
	ErrAOutScanNotStarted = errors.New("usb1808: analog output scan not started")
	ErrAOutScanStopped    = errors.New("usb1808: analog output scan already stopped")
	ErrAOutUnderrun       = errors.New("usb1808: analog output FIFO underrun")
	ErrInvalidAOutQueue   = errors.New("usb1808: invalid AOut scan queue")
)
