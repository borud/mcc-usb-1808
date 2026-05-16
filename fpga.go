package usb1808

import (
	"fmt"

	"github.com/borud/mcc-usb-1808/v3/internal/firmware"
)

// Init performs the full device initialization sequence. It checks whether the
// FPGA image has been loaded, and it it hasn't, takes care of loading it. This
// means that the first time we run Init after the device has been booted this
// will take a few seconds.  It then builds the calibration tables from EPROM.
func (d *Device) Init() error {
	// Check FPGA status.
	status, err := d.Status()
	if err != nil {
		return err
	}

	if !status.FPGAConfigured() {
		if err := d.fpgaLoad(firmware.Image); err != nil {
			return err
		}
	}

	// Build calibration tables.
	if err := d.buildAInCalibrationTable(); err != nil {
		return fmt.Errorf("build AIn cal table: %w", err)
	}
	if err := d.buildAOutCalibrationTable(); err != nil {
		return fmt.Errorf("build AOut cal table: %w", err)
	}

	d.initialized.Store(true)
	return nil
}

// fpgaConfig puts the device into FPGA configuration mode.
func (d *Device) fpgaConfig() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdFPGAConfig, 0, 0, []byte{fpgaUnlockCode})
}

// fpgaData sends up to 64 bytes of FPGA configuration data.
func (d *Device) fpgaData(data []byte) error {
	if len(data) > 64 {
		return fmt.Errorf("usb1808: FPGA data chunk too large: %d bytes (max 64)", len(data))
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdFPGAData, 0, 0, data)
}

// fpgaLoad loads an FPGA image onto the device.
// The image is sent in 64-byte chunks. The device must be put into
// configuration mode first, and FPGA_CONFIGURED status verified after.
func (d *Device) fpgaLoad(image []byte) error {
	// Enter configuration mode.
	if err := d.fpgaConfig(); err != nil {
		return fmt.Errorf("FPGA config mode: %w", err)
	}

	// Verify configuration mode.
	status, err := d.Status()
	if err != nil {
		return err
	}
	if !status.FPGAConfigMode() {
		return fmt.Errorf("usb1808: device did not enter FPGA config mode")
	}

	d.log.Info("loading FPGA image", "bytes", len(image))

	// Stream image in 64-byte chunks, logging progress every 256 KiB.
	const logInterval = 256 * 1024
	nextLog := logInterval
	for offset := 0; offset < len(image); offset += 64 {
		end := offset + 64
		if end > len(image) {
			end = len(image)
		}
		if err := d.fpgaData(image[offset:end]); err != nil {
			return fmt.Errorf("FPGA data at offset %d: %w", offset, err)
		}
		if offset >= nextLog {
			d.log.Debug("FPGA load progress", "bytes_sent", offset, "total", len(image))
			nextLog += logInterval
		}
	}

	// Verify FPGA is now configured.
	status, err = d.Status()
	if err != nil {
		return err
	}
	if !status.FPGAConfigured() {
		return ErrFPGANotConfigured
	}

	d.log.Info("FPGA image loaded")
	return nil
}
