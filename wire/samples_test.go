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

func TestUint16sToBytesInto(t *testing.T) {
	samples := []uint16{0x1234, 0x5678, 0xABCD}

	// Exactly fits.
	dst := make([]byte, 6)
	n := Uint16sToBytesInto(dst, samples)
	if n != 6 {
		t.Fatalf("n = %d, want 6", n)
	}
	if dst[0] != 0x34 || dst[1] != 0x12 {
		t.Errorf("sample 0: got 0x%02x%02x, want 0x3412", dst[1], dst[0])
	}

	// Partial: buf holds only 2 samples.
	dst2 := make([]byte, 4)
	n = Uint16sToBytesInto(dst2, samples)
	if n != 4 {
		t.Fatalf("n = %d, want 4", n)
	}

	// Buffer too small for even one sample.
	dst3 := make([]byte, 1)
	n = Uint16sToBytesInto(dst3, samples)
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}

	// Empty samples.
	n = Uint16sToBytesInto(dst, nil)
	if n != 0 {
		t.Fatalf("n = %d, want 0 for nil samples", n)
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
