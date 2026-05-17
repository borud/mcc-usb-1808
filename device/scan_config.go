package device

import "fmt"

// ScanConfig holds configuration for an analog input scan.
type ScanConfig struct {
	Channels []ChannelConfig // Scan queue entries.
	Rate     int             // Sample rate in Hz per channel.
	Count    uint32          // Total number of scans (0 = continuous).
	Options  uint8           // Scan option flags.
}

// ScanOption configures optional parameters for CreateScan.
type ScanOption func(*scanOptions)

type scanOptions struct {
	pipelineDepth     int
	concurrentReaders int
}

// DefaultPipelineDepth is the number of bulk read results to buffer ahead
// of the consumer.
const DefaultPipelineDepth = 32

// DefaultConcurrentReaders is the number of goroutines concurrently issuing
// synchronous USB bulk reads.
const DefaultConcurrentReaders = 4

// WithPipelineDepth sets the pipeline depth (buffered read-ahead batches).
func WithPipelineDepth(n int) ScanOption {
	return func(o *scanOptions) {
		if n > 0 {
			o.pipelineDepth = n
		}
	}
}

// WithConcurrentReaders sets the number of concurrent reader goroutines.
func WithConcurrentReaders(n int) ScanOption {
	return func(o *scanOptions) {
		if n > 0 {
			o.concurrentReaders = n
		}
	}
}

// validateScanConfig checks the scan configuration for errors.
func validateScanConfig(cfg ScanConfig) error {
	if len(cfg.Channels) == 0 || len(cfg.Channels) > MaxAInQueue {
		return fmt.Errorf("%w: queue length %d (max %d)", ErrInvalidChannel, len(cfg.Channels), MaxAInQueue)
	}
	if cfg.Rate <= 0 {
		return fmt.Errorf("usb1808: invalid scan rate: %d", cfg.Rate)
	}
	for _, ch := range cfg.Channels {
		if ch.Index < 0 || ch.Index > 12 {
			return fmt.Errorf("%w: scan queue index %d", ErrInvalidChannel, ch.Index)
		}
		if ch.Type == ChannelTypeAnalog {
			if ch.Index >= NumAInChannels {
				return fmt.Errorf("%w: analog channel %d", ErrInvalidChannel, ch.Index)
			}
			if ch.Range > UP5V {
				return fmt.Errorf("%w: %d", ErrInvalidRange, ch.Range)
			}
			if ch.Mode == 2 {
				return fmt.Errorf("%w: mode code 2 is undefined", ErrInvalidMode)
			}
		}
	}
	return nil
}
