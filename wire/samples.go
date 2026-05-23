package wire

import "encoding/binary"

// Uint16sToBytes converts a slice of uint16 values to a little-endian byte
// slice. Each sample occupies two bytes.
func Uint16sToBytes(samples []uint16) []byte {
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], s)
	}
	return buf
}

// BytesToUint16s converts a little-endian byte slice to a slice of uint16
// values. len(data) must be even; odd trailing bytes are ignored.
func BytesToUint16s(data []byte) []uint16 {
	n := len(data) / 2
	out := make([]uint16, n)
	for i := range n {
		out[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return out
}
