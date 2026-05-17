package device

import "github.com/borud/mcc-usb-1808/v4/wire"

// memSetAddress sets the EEPROM address pointer. Caller must hold d.mu.
func (d *Device) memSetAddress(addr uint16) error {
	return d.transport.ControlOut(cmdMemAddress, 0, 0, wire.PutUint16LE(addr))
}

// memRead reads length bytes from EEPROM at the current address. Caller must hold d.mu.
func (d *Device) memRead(length int) ([]byte, error) {
	return d.transport.ControlIn(cmdMemory, 0, 0, length)
}
