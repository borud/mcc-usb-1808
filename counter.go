package usb1808

import (
	"fmt"

	"github.com/borud/mcc-usb-1808/v3/internal/wire"
)

func validateCounter(counter int) error {
	if counter < 0 || counter >= NumCounters {
		return fmt.Errorf("%w: %d", ErrInvalidCounter, counter)
	}
	return nil
}

// ReadCounter reads the 32-bit value of the specified counter (0-3).
func (d *Device) ReadCounter(counter int) (uint32, error) {
	if err := validateCounter(counter); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdCounter, 0, uint16(counter), 4)
	if err != nil {
		return 0, err
	}
	return wire.Uint32LE(data), nil
}

// WriteCounter sets the 32-bit value of the specified counter.
func (d *Device) WriteCounter(counter int, value uint32) error {
	if err := validateCounter(counter); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdCounter, 0, uint16(counter), wire.PutUint32LE(value))
}

// CounterOptions reads the options byte for the specified counter.
func (d *Device) CounterOptions(counter int) (uint8, error) {
	if err := validateCounter(counter); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdCounterOpts, 0, uint16(counter), 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// SetCounterOptions sets the options byte for the specified counter.
// For counters 0-1, use CounterClearOnRead, CounterNoRecycle, etc.
// For encoders 2-3, use EncoderX1, EncoderClearOnZ, etc.
func (d *Device) SetCounterOptions(counter int, options uint8) error {
	if err := validateCounter(counter); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdCounterOpts, uint16(options), uint16(counter), nil)
}

// CounterLimits reads the 32-bit limit value for the specified counter.
// index: 0 = minimum, 1 = maximum.
func (d *Device) CounterLimits(counter int, index uint16) (uint32, error) {
	if err := validateCounter(counter); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdCounterLimits, index, uint16(counter), 4)
	if err != nil {
		return 0, err
	}
	return wire.Uint32LE(data), nil
}

// SetCounterLimits sets the 32-bit limit value for the specified counter.
// index: 0 = minimum, 1 = maximum.
func (d *Device) SetCounterLimits(counter int, index uint16, value uint32) error {
	if err := validateCounter(counter); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdCounterLimits, index, uint16(counter), wire.PutUint32LE(value))
}

// CounterMode reads the mode byte for the specified counter.
func (d *Device) CounterMode(counter int) (uint8, error) {
	if err := validateCounter(counter); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdCounterMode, 0, uint16(counter), 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// SetCounterMode sets the mode byte for the specified counter.
func (d *Device) SetCounterMode(counter int, mode uint8) error {
	if err := validateCounter(counter); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdCounterMode, uint16(mode), uint16(counter), nil)
}

// CounterParams reads the 2-byte counter parameters (mode + options combined).
func (d *Device) CounterParams(counter int) ([]byte, error) {
	if err := validateCounter(counter); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlIn(cmdCounterParam, 0, uint16(counter), 2)
}

// SetCounterParams writes the 2-byte counter parameters.
func (d *Device) SetCounterParams(counter int, data []byte) error {
	if err := validateCounter(counter); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdCounterParam, 0, uint16(counter), data)
}
