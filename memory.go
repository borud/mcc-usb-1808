package usb1808

import "github.com/borud/mcc-usb-1808/v3/internal/wire"

// memSetAddress sets the EEPROM address pointer for subsequent memory operations.
func (d *Device) memSetAddress(addr uint16) error {
	return d.transport.ControlOut(cmdMemAddress, 0, 0, wire.PutUint16LE(addr))
}

// memRead reads length bytes from EEPROM at the current address.
func (d *Device) memRead(length int) ([]byte, error) {
	return d.transport.ControlIn(cmdMemory, 0, 0, length)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) memWrite(data []byte) error {
	return d.transport.ControlOut(cmdMemory, 0, 0, data)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) memoryRead(addr uint16, length int) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.memSetAddress(addr); err != nil {
		return nil, err
	}
	return d.memRead(length)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) memoryWrite(addr uint16, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.memSetAddress(addr); err != nil {
		return err
	}
	return d.memWrite(data)
}

//lint:ignore U1000 device capability, not yet wired to public API
func (d *Device) memWriteEnable() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdMemWriteEn, 0, 0, []byte{fpgaUnlockCode})
}
