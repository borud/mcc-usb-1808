package usb1808

import "github.com/borud/mcc-usb-1808/internal/wire"

// Status represents the device status word.
type Status uint16

// AInScanRunning reports whether the analog input scan pacer is active.
func (s Status) AInScanRunning() bool { return s&StatusAInScanRunning != 0 }

// AInScanOverrun reports whether the analog input FIFO has overrun.
func (s Status) AInScanOverrun() bool { return s&StatusAInScanOverrun != 0 }

// AOutScanRunning reports whether the analog output scan is active.
func (s Status) AOutScanRunning() bool { return s&StatusAOutScanRunning != 0 }

// AOutScanUnderrun reports whether the analog output FIFO has underrun.
func (s Status) AOutScanUnderrun() bool { return s&StatusAOutScanUnder != 0 }

// AInScanDone reports whether the analog input scan has completed.
func (s Status) AInScanDone() bool { return s&StatusAInScanDone != 0 }

// AOutScanDone reports whether the analog output scan has completed.
func (s Status) AOutScanDone() bool { return s&StatusAOutScanDone != 0 }

// FPGAConfigured reports whether the FPGA is configured and ready.
func (s Status) FPGAConfigured() bool { return s&StatusFPGAConfigured != 0 }

// FPGAConfigMode reports whether the device is in FPGA configuration mode.
func (s Status) FPGAConfigMode() bool { return s&StatusFPGAConfigMode != 0 }

// Status reads the 16-bit device status word.
func (d *Device) Status() (Status, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdStatus, 0, 0, 2)
	if err != nil {
		return 0, err
	}
	return Status(wire.Uint16LE(data)), nil
}
