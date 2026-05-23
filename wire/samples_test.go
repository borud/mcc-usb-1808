package wire

import (
	"testing"
)

func TestUint16sToBytes(t *testing.T) {
	samples := []uint16{0x0000, 0x8000, 0xFFFF, 0x1234}
	b := Uint16sToBytes(samples)
	want := []byte{0x00, 0x00, 0x00, 0x80, 0xFF, 0xFF, 0x34, 0x12}
	if len(b) != len(want) {
		t.Fatalf("len = %d, want %d", len(b), len(want))
	}
	for i, v := range want {
		if b[i] != v {
			t.Errorf("b[%d] = 0x%02x, want 0x%02x", i, b[i], v)
		}
	}
	// Round-trip.
	back := BytesToUint16s(b)
	if len(back) != len(samples) {
		t.Fatalf("round-trip len = %d, want %d", len(back), len(samples))
	}
	for i, s := range samples {
		if back[i] != s {
			t.Errorf("back[%d] = 0x%04x, want 0x%04x", i, back[i], s)
		}
	}
}

func TestBytesToUint16s_OddLength(t *testing.T) {
	// Odd byte ignored.
	b := []byte{0x01, 0x00, 0xFF}
	out := BytesToUint16s(b)
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
	if out[0] != 1 {
		t.Errorf("out[0] = %d, want 1", out[0])
	}
}
