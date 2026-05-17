package wire

import (
	"bytes"
	"testing"
)

func TestAInScanStartPayload(t *testing.T) {
	// Golden byte test from spec: 1000 scans at 100 kHz, block mode, no trigger.
	// scan_count=1000, retrig=0, pacer_period=999, packet_size=0xFF, options=0x00
	got := AInScanStartPayload(1000, 0, 999, 0xFF, 0x00)
	want := []byte{
		0xE8, 0x03, 0x00, 0x00, // scan_count = 1000
		0x00, 0x00, 0x00, 0x00, // retrig_count = 0
		0xE7, 0x03, 0x00, 0x00, // pacer_period = 999
		0xFF,                   // packet_size = 255
		0x00,                   // options = 0
	}
	if !bytes.Equal(got, want) {
		t.Errorf("AInScanStartPayload mismatch:\ngot:  %x\nwant: %x", got, want)
	}
}

func TestAOutScanStartPayload(t *testing.T) {
	// 13 bytes, no packet_size field.
	got := AOutScanStartPayload(0, 0, 19530, 0x00)
	if len(got) != 13 {
		t.Errorf("AOutScanStartPayload length = %d, want 13", len(got))
	}
	// Verify pacer_period field at offset 8-11.
	period := Uint32LE(got[8:12])
	if period != 19530 {
		t.Errorf("pacer_period = %d, want 19530", period)
	}
}

func TestPacerPeriod(t *testing.T) {
	tests := []struct {
		freq float64
		want uint32
	}{
		{100000, 999},  // 100 kHz
		{50000, 1999},  // 50 kHz
		{200000, 499},  // 200 kHz
		{0, 0},         // external clock
		{-1, 0},        // external clock
	}
	for _, tt := range tests {
		got := PacerPeriod(tt.freq)
		if got != tt.want {
			t.Errorf("PacerPeriod(%v) = %d, want %d", tt.freq, got, tt.want)
		}
	}
}

func TestTimerParams(t *testing.T) {
	// frequency=1000, dutyCycle=0.5 → period=99999, pulseWidth=49999
	period, pw := TimerParams(1000, 0.5)
	if period != 99999 {
		t.Errorf("period = %d, want 99999", period)
	}
	if pw != 49999 {
		t.Errorf("pulseWidth = %d, want 49999", pw)
	}
}
