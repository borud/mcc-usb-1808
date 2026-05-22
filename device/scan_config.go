package device

import "fmt"

// ScanConfig holds configuration for an analog input scan.
type ScanConfig struct {
	// Channels lists the scan queue entries.
	Channels []ChannelConfig
	// Rate is the sample rate in Hz per channel.
	Rate int
	// Count is the total number of scans (0 = continuous).
	Count uint32
	// Options holds scan option flags.
	Options uint8
}

// ScanOption configures optional parameters for CreateScan.
type ScanOption func(*scanOptions)

type scanOptions struct {
	pipelineDepth int
}

// DefaultPipelineDepth is the number of bulk read results to buffer ahead
// of the consumer.
const DefaultPipelineDepth = 32

// WithPipelineDepth sets the pipeline depth (buffered read-ahead batches).
func WithPipelineDepth(n int) ScanOption {
	return func(o *scanOptions) {
		if n > 0 {
			o.pipelineDepth = n
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
		switch ch.Type {
		case ChannelTypeAnalog:
			if ch.Index >= NumAInChannels {
				return fmt.Errorf("%w: analog channel %d", ErrInvalidChannel, ch.Index)
			}
			if ch.Range > UP5V {
				return fmt.Errorf("%w: %d", ErrInvalidRange, ch.Range)
			}
			if ch.Mode != Differential && ch.Mode != SingleEnded && ch.Mode != Grounded {
				return fmt.Errorf("%w: mode code %d", ErrInvalidMode, ch.Mode)
			}
		case ChannelTypeDIO, ChannelTypeCounter, ChannelTypeEncoder:
			if ch.Range != 0 || ch.Mode != 0 {
				return fmt.Errorf("%w: range/mode must be zero for %s channel", ErrInvalidChannel, ch.Type)
			}
		default:
			return fmt.Errorf("%w: unknown channel type %d", ErrInvalidChannel, ch.Type)
		}
	}
	return nil
}
