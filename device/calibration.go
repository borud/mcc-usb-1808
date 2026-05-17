package device

import (
	"time"

	"github.com/borud/mcc-usb-1808/v4/wire"
)

// buildAInCalibrationTable reads ADC calibration coefficients from EEPROM.
func (d *Device) buildAInCalibrationTable() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for ch := range NumAInChannels {
		for gain := range NumAInRanges {
			addr := uint16(memADCCalBase) + uint16((ch*4+gain)*8)

			if err := d.memSetAddress(addr); err != nil {
				return err
			}
			slopeData, err := d.memRead(4)
			if err != nil {
				return err
			}

			if err := d.memSetAddress(addr + 4); err != nil {
				return err
			}
			offsetData, err := d.memRead(4)
			if err != nil {
				return err
			}

			d.calAIn[ch][gain] = Calibration{
				Slope:  wire.Float32LE(slopeData),
				Offset: wire.Float32LE(offsetData),
			}
		}
	}
	return nil
}

// buildAOutCalibrationTable reads DAC calibration coefficients from EEPROM.
func (d *Device) buildAOutCalibrationTable() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for ch := range NumAOutChannels {
		addr := uint16(memDACCalBase) + uint16(ch*8)

		if err := d.memSetAddress(addr); err != nil {
			return err
		}
		slopeData, err := d.memRead(4)
		if err != nil {
			return err
		}

		if err := d.memSetAddress(addr + 4); err != nil {
			return err
		}
		offsetData, err := d.memRead(4)
		if err != nil {
			return err
		}

		d.calAOut[ch] = Calibration{
			Slope:  wire.Float32LE(slopeData),
			Offset: wire.Float32LE(offsetData),
		}
	}
	return nil
}

// CalibrationDate reads the factory calibration date from EEPROM.
func (d *Device) CalibrationDate() (time.Time, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.memSetAddress(memCalDate); err != nil {
		return time.Time{}, err
	}
	data, err := d.memRead(6)
	if err != nil {
		return time.Time{}, err
	}

	year := int(data[0]) + 2000
	month := time.Month(data[1])
	day := int(data[2])
	hour := int(data[3])
	minute := int(data[4])
	second := int(data[5])

	return time.Date(year, month, day, hour, minute, second, 0, time.UTC), nil
}

// CalibrationTable returns the analog input calibration table.
func (d *Device) CalibrationTable() CalibrationTable {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calAIn
}

// AnalogOutCalTable returns the analog output calibration table.
func (d *Device) AnalogOutCalTable() [NumAOutChannels]Calibration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calAOut
}
