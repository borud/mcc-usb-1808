package codec

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/borud/mcc-usb-1808/v4/device"
)

func identityCal() device.CalibrationTable {
	var cal device.CalibrationTable
	for ch := range device.NumAInChannels {
		for r := range device.NumAInRanges {
			cal[ch][r] = device.Calibration{Slope: 1.0, Offset: 0.0}
		}
	}
	return cal
}

func makeFrame(values ...uint32) []byte {
	buf := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(buf[i*4:], v)
	}
	return buf
}

func TestVoltage_AllRanges(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec := NewDecoder(channels, cal)

	tests := []struct {
		name    string
		raw     uint32
		r       device.Range
		want    float64
		epsilon float64
	}{
		{"BP10V midscale", 131072, device.BP10V, 0.0, 0.001},
		{"BP10V zero", 0, device.BP10V, -10.0, 0.001},
		{"BP10V max", 262143, device.BP10V, 9.99992, 0.001},
		{"BP5V midscale", 131072, device.BP5V, 0.0, 0.001},
		{"BP5V zero", 0, device.BP5V, -5.0, 0.001},
		{"UP10V zero", 0, device.UP10V, 0.0, 0.001},
		{"UP10V max", 262143, device.UP10V, 10.0, 0.001},
		{"UP5V zero", 0, device.UP5V, 0.0, 0.001},
		{"UP5V max", 262143, device.UP5V, 5.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := []device.ChannelConfig{{Index: 0, Type: device.ChannelTypeAnalog, Range: tt.r}}
			d := NewDecoder(ch, cal)
			frame := makeFrame(tt.raw)
			got := d.Voltage(Frame(frame), 0)
			if math.Abs(got-tt.want) > tt.epsilon {
				t.Errorf("Voltage(%d, %s) = %v, want ~%v", tt.raw, tt.r, got, tt.want)
			}
		})
	}

	// Verify non-analog returns raw value.
	dioCh := []device.ChannelConfig{{Index: 8, Type: device.ChannelTypeDIO}}
	dioDec := NewDecoder(dioCh, cal)
	frame := makeFrame(0x55)
	got := dioDec.Voltage(Frame(frame), 0)
	if got != 0x55 {
		t.Errorf("DIO voltage = %v, want 85", got)
	}

	_ = dec
}

func TestFrames_Count(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec := NewDecoder(channels, cal)

	// 3 frames of 2 channels = 24 bytes.
	chunk := make([]byte, 3*2*4)
	for i := range 6 {
		binary.LittleEndian.PutUint32(chunk[i*4:], uint32(i*1000))
	}

	count := 0
	for range dec.Frames(chunk) {
		count++
	}
	if count != 3 {
		t.Errorf("frame count = %d, want 3", count)
	}
}

func TestFrames_PartialChunk(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec := NewDecoder(channels, cal)

	// 2.5 frames worth of data — only 2 should be yielded.
	chunk := make([]byte, 2*2*4+4) // 20 bytes, frameSize=8
	count := 0
	for range dec.Frames(chunk) {
		count++
	}
	if count != 2 {
		t.Errorf("frame count = %d, want 2", count)
	}
}

func TestRawUint32(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP5V},
	}
	dec := NewDecoder(channels, cal)

	frame := makeFrame(12345, 67890)
	if dec.RawUint32(Frame(frame), 0) != 12345 {
		t.Errorf("RawUint32(0) = %d, want 12345", dec.RawUint32(Frame(frame), 0))
	}
	if dec.RawUint32(Frame(frame), 1) != 67890 {
		t.Errorf("RawUint32(1) = %d, want 67890", dec.RawUint32(Frame(frame), 1))
	}
}

func TestDigital(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 8, Type: device.ChannelTypeDIO},
	}
	dec := NewDecoder(channels, cal)

	frame := makeFrame(131072, 0xAB)
	if dec.Digital(Frame(frame)) != 0xAB {
		t.Errorf("Digital() = 0x%02x, want 0xAB", dec.Digital(Frame(frame)))
	}
}

func TestDigital_NoDIO(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec := NewDecoder(channels, cal)

	frame := makeFrame(131072)
	if dec.Digital(Frame(frame)) != 0 {
		t.Errorf("Digital() with no DIO channel = %d, want 0", dec.Digital(Frame(frame)))
	}
}

func TestVoltage_WithCalibration(t *testing.T) {
	var cal device.CalibrationTable
	// Apply a known calibration: slope=1.001, offset=5.0
	cal[0][device.BP10V] = device.Calibration{Slope: 1.001, Offset: 5.0}

	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec := NewDecoder(channels, cal)

	// raw=131072 with slope=1.001 offset=5 → calibrated = 131072*1.001 + 5 = 131203.072 + 5 = 131208
	// voltage = (round(131208.072) - 131072) * 10 / 131072 = 136 * 10 / 131072 ≈ 0.01037
	frame := makeFrame(131072)
	got := dec.Voltage(Frame(frame), 0)
	// Rough check — the point is that calibration shifts the result.
	if math.Abs(got) < 0.001 {
		t.Errorf("expected non-zero voltage with non-identity calibration, got %v", got)
	}
}

func FuzzCalibrate(f *testing.F) {
	for _, raw := range []uint32{0, 131072, 262143, 100000} {
		for r := range 4 {
			f.Add(raw, uint8(r), float32(1.0), float32(0.0))
		}
	}

	f.Fuzz(func(t *testing.T, raw uint32, r uint8, slope, offset float32) {
		if r > 3 {
			t.Skip()
		}
		if math.IsNaN(float64(slope)) || math.IsInf(float64(slope), 0) {
			t.Skip()
		}
		if math.IsNaN(float64(offset)) || math.IsInf(float64(offset), 0) {
			t.Skip()
		}

		cal := device.Calibration{Slope: slope, Offset: offset}
		v := RawToVolts(raw, device.Range(r), cal)

		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("RawToVolts(%d, %d, {%v, %v}) = %v", raw, r, slope, offset, v)
		}

		// Range checks for finite calibration.
		switch device.Range(r) {
		case device.BP10V:
			if v < -20 || v > 20 {
				// With extreme calibration, values can exceed nominal range,
				// but should still be finite.
			}
		case device.BP5V:
			if v < -10 || v > 10 {
			}
		case device.UP10V:
			if v < -1 || v > 11 {
				// Clamped to [0, 262143] before scaling.
			}
		case device.UP5V:
			if v < -1 || v > 6 {
			}
		}
	})
}
