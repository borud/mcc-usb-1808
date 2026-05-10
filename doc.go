// Package usb1808 provides a Go driver for the Measurement Computing
// USB-1808 and USB-1808X multifunction DAQ devices.
//
// The library communicates with the device over USB using vendor control
// transfers and bulk endpoints. All operations are methods on the [Device]
// type, obtained via [Open].
//
// # Quick Start
//
//	dev, err := usb1808.Open()
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer dev.Close()
//
//	if err := dev.Init(); err != nil {
//		log.Fatal(err)
//	}
//
//	serial, err := dev.SerialNumber()
//	fmt.Println("Serial:", serial)
package usb1808
