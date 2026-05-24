package device

import (
	"fmt"
	"time"
)

// AOutScanConfig holds configuration for an analog output scan.
type AOutScanConfig struct {
	// Channels is the scan queue. Each value must be 0 (DAC0) or 1 (DAC1).
	Channels []int
	// Rate is the output rate in Hz per channel.
	Rate int
	// Count is the total number of scans to output. 0 means loop forever.
	Count uint32
	// Options holds scan option flags (ScanOptExternalTrigger etc.).
	Options uint8
}

// AOutScanOption configures optional parameters for CreateAOutScan.
type AOutScanOption func(*aoutScanOptions)

// UnderrunFunc is called when a FIFO underrun is detected and recovered.
// framesPlayed is the estimated number of output frames before the underrun.
// Called from the scan goroutine; must not block for long.
type UnderrunFunc func(framesPlayed uint64, ts time.Time)

type aoutScanOptions struct {
	pipelineDepth  int
	onUnderrun     UnderrunFunc
	stopOnUnderrun bool
}

// DefaultAOutPipelineDepth is the default number of bulk OUT URB slots.
const DefaultAOutPipelineDepth = 32

// WithAOutPipelineDepth sets the number of bulk OUT URB slots.
func WithAOutPipelineDepth(n int) AOutScanOption {
	return func(o *aoutScanOptions) {
		if n > 0 {
			o.pipelineDepth = n
		}
	}
}

// WithUnderrunCallback registers a function called on each tolerated underrun.
func WithUnderrunCallback(fn UnderrunFunc) AOutScanOption {
	return func(o *aoutScanOptions) { o.onUnderrun = fn }
}

// WithStopOnUnderrun causes the scan to fail immediately on the first underrun
// instead of attempting recovery.
func WithStopOnUnderrun() AOutScanOption {
	return func(o *aoutScanOptions) { o.stopOnUnderrun = true }
}

func validateAOutScanConfig(cfg AOutScanConfig, model Model) error {
	if len(cfg.Channels) == 0 || len(cfg.Channels) > MaxAOutQueue {
		return fmt.Errorf("%w: queue length %d (max %d)",
			ErrInvalidAOutQueue, len(cfg.Channels), MaxAOutQueue)
	}
	seen := map[int]bool{}
	for _, ch := range cfg.Channels {
		if ch < 0 || ch >= NumAOutChannels {
			return fmt.Errorf("%w: channel %d", ErrInvalidChannel, ch)
		}
		if seen[ch] {
			return fmt.Errorf("%w: channel %d appears twice", ErrInvalidAOutQueue, ch)
		}
		seen[ch] = true
	}
	if cfg.Rate <= 0 {
		return fmt.Errorf("usb1808: invalid AOut scan rate: %d", cfg.Rate)
	}
	maxRate := 125_000
	if model == USB1808X {
		maxRate = 500_000
	}
	if cfg.Rate > maxRate {
		return fmt.Errorf("usb1808: AOut rate %d exceeds model max %d", cfg.Rate, maxRate)
	}
	return nil
}
