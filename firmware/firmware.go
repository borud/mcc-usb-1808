// Package firmware holds the embedded FPGA bitstream for USB-1808 devices.
package firmware

import _ "embed"

// Image is the embedded FPGA bitstream loaded during device initialization.
//
//go:embed usb-1808.bin
var Image []byte
