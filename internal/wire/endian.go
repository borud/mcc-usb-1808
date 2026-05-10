// Package wire handles wire-level byte serialization for the
// USB-1808/1808X protocol. It has zero USB or cgo dependencies.
package wire

import (
	"encoding/binary"
	"math"
)

// PutUint16LE encodes v as a 2-byte little-endian slice.
func PutUint16LE(v uint16) []byte {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return b
}

// PutUint32LE encodes v as a 4-byte little-endian slice.
func PutUint32LE(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

// PutFloat32LE encodes v as a 4-byte little-endian IEEE-754 slice.
func PutFloat32LE(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

// Uint16LE decodes a 2-byte little-endian value.
func Uint16LE(b []byte) uint16 {
	return binary.LittleEndian.Uint16(b)
}

// Uint32LE decodes a 4-byte little-endian value.
func Uint32LE(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b)
}

// Float32LE decodes a 4-byte little-endian IEEE-754 float.
func Float32LE(b []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}
