package usb1808

import "github.com/borud/mcc-usb-1808/internal/wire"

// DigitalDirection reads the digital port tristate register.
// A '1' bit means the corresponding pin is an input.
func (d *Device) DigitalDirection() (uint16, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdDTristate, 0, 0, 2)
	if err != nil {
		return 0, err
	}
	return wire.Uint16LE(data), nil
}

// SetDigitalDirection sets the digital port tristate register.
// A '1' bit makes the corresponding pin an input, '0' makes it an output.
func (d *Device) SetDigitalDirection(value uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdDTristate, value, 0, nil)
}

// ReadDigital reads the current state of the digital input pins.
func (d *Device) ReadDigital() (uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdDPort, 0, 0, 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// DigitalLatch reads the digital port output latch register.
func (d *Device) DigitalLatch() (uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdDLatch, 0, 0, 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// WriteDigital sets the digital port output latch register.
func (d *Device) WriteDigital(value uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdDLatch, uint16(value), 0, nil)
}
