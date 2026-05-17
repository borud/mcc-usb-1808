package wire

import (
	"bytes"
	"testing"
)

func TestPutUint16LE(t *testing.T) {
	tests := []struct {
		val  uint16
		want []byte
	}{
		{0, []byte{0x00, 0x00}},
		{1, []byte{0x01, 0x00}},
		{0x0100, []byte{0x00, 0x01}},
		{0xFFFF, []byte{0xFF, 0xFF}},
		{0x7000, []byte{0x00, 0x70}},
	}
	for _, tt := range tests {
		got := PutUint16LE(tt.val)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("PutUint16LE(%d) = %x, want %x", tt.val, got, tt.want)
		}
		// Round-trip.
		if v := Uint16LE(got); v != tt.val {
			t.Errorf("Uint16LE(%x) = %d, want %d", got, v, tt.val)
		}
	}
}

func TestPutUint32LE(t *testing.T) {
	tests := []struct {
		val  uint32
		want []byte
	}{
		{0, []byte{0x00, 0x00, 0x00, 0x00}},
		{999, []byte{0xE7, 0x03, 0x00, 0x00}},
		{1000, []byte{0xE8, 0x03, 0x00, 0x00}},
		{0xFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
	}
	for _, tt := range tests {
		got := PutUint32LE(tt.val)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("PutUint32LE(%d) = %x, want %x", tt.val, got, tt.want)
		}
		if v := Uint32LE(got); v != tt.val {
			t.Errorf("Uint32LE(%x) = %d, want %d", got, v, tt.val)
		}
	}
}

func TestPutFloat32LE(t *testing.T) {
	tests := []struct {
		val  float32
		want []byte
	}{
		{1.0, []byte{0x00, 0x00, 0x80, 0x3F}},
		{0.0, []byte{0x00, 0x00, 0x00, 0x00}},
	}
	for _, tt := range tests {
		got := PutFloat32LE(tt.val)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("PutFloat32LE(%v) = %x, want %x", tt.val, got, tt.want)
		}
		if v := Float32LE(got); v != tt.val {
			t.Errorf("Float32LE(%x) = %v, want %v", got, v, tt.val)
		}
	}
}
