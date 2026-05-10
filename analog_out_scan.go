package usb1808

import (
	"fmt"

	"github.com/borud/mcc-usb-1808/internal/wire"
)

// AnalogOutScanConfig holds configuration for an analog output scan.
type AnalogOutScanConfig struct {
	Channels    []int   // Scan queue channel selectors (0-2).
	Rate        float64 // Sample rate in Hz.
	Count       uint32  // Total number of scans (0 = continuous).
	RetrigCount uint32  // Scans per retrigger.
	Options     uint8   // Scan option flags.
}

// ConfigureAnalogOutScan writes the output scan queue configuration.
func (d *Device) ConfigureAnalogOutScan(channels []int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(channels) == 0 || len(channels) > MaxAOutQueue {
		return fmt.Errorf("%w: queue length %d (max %d)", ErrInvalidChannel, len(channels), MaxAOutQueue)
	}

	buf := make([]byte, MaxAOutQueue)
	for i, ch := range channels {
		if ch < 0 || ch > 2 {
			return fmt.Errorf("%w: output queue value %d", ErrInvalidChannel, ch)
		}
		buf[i] = uint8(ch)
	}
	lastChan := uint16(len(channels))
	return d.transport.ControlOut(cmdAOutScanConf, 0, lastChan-1, buf)
}

// AnalogOutScanConfig reads the current output scan queue configuration.
func (d *Device) AnalogOutScanConfig(numChannels int) ([]uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdAOutScanConf, 0, uint16(numChannels-1), MaxAOutQueue)
	if err != nil {
		return nil, err
	}
	return data[:numChannels], nil
}

// StartAnalogOutScan starts an analog output scan.
func (d *Device) StartAnalogOutScan(cfg AnalogOutScanConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	pacerPeriod := wire.PacerPeriod(cfg.Rate)
	payload := wire.AOutScanStartPayload(cfg.Count, cfg.RetrigCount, pacerPeriod, cfg.Options)
	return d.transport.ControlOut(cmdAOutScanStart, 0, 0, payload)
}

// WriteAnalogOutScan sends output scan data to the bulk OUT endpoint.
// Data should be 16-bit LE samples, interleaved by queue position.
func (d *Device) WriteAnalogOutScan(data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.BulkWrite(epBulkOut, data, usbTimeout)
}

// StopAnalogOutScan stops a running analog output scan.
func (d *Device) StopAnalogOutScan() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdAOutScanStop, 0, 0, nil)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) analogOutClearFIFO() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transport.ControlOut(cmdAOutClearFIFO, 0, 0, nil)
}

