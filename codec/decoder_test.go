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

	tests := []struct {
		name    string
		raw     uint32
		r       device.Range
		want    float64
		epsilon float64
	}{
		{"BP10V midscale", device.ADCMidscale, device.BP10V, 0.0, 0.001},
		{"BP10V zero", 0, device.BP10V, -10.0, 0.001},
		{"BP10V max", device.ADCMaxCode, device.BP10V, 9.99992, 0.001},
		{"BP5V midscale", device.ADCMidscale, device.BP5V, 0.0, 0.001},
		{"BP5V zero", 0, device.BP5V, -5.0, 0.001},
		{"UP10V zero", 0, device.UP10V, 0.0, 0.001},
		{"UP10V max", device.ADCMaxCode, device.UP10V, 10.0, 0.001},
		{"UP5V zero", 0, device.UP5V, 0.0, 0.001},
		{"UP5V max", device.ADCMaxCode, device.UP5V, 5.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := []device.ChannelConfig{{Index: 0, Type: device.ChannelTypeAnalog, Range: tt.r}}
			d, _ := NewDecoder(ch, cal)
			chunk := makeFrame(tt.raw)
			for frame := range d.Frames(chunk) {
				got := frame.Voltage(0)
				if math.Abs(got-tt.want) > tt.epsilon {
					t.Errorf("Voltage(%d, %s) = %v, want ~%v", tt.raw, tt.r, got, tt.want)
				}
			}
		})
	}

	// Verify non-analog returns raw value.
	dioCh := []device.ChannelConfig{{Index: 8, Type: device.ChannelTypeDIO}}
	dioDec, _ := NewDecoder(dioCh, cal)
	chunk := makeFrame(0x55)
	for frame := range dioDec.Frames(chunk) {
		got := frame.Voltage(0)
		if got != 0x55 {
			t.Errorf("DIO voltage = %v, want 85", got)
		}
	}
}

func TestFrames_Count(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec, _ := NewDecoder(channels, cal)

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
	dec, _ := NewDecoder(channels, cal)

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
	dec, _ := NewDecoder(channels, cal)

	chunk := makeFrame(12345, 67890)
	for frame := range dec.Frames(chunk) {
		if frame.RawUint32(0) != 12345 {
			t.Errorf("RawUint32(0) = %d, want 12345", frame.RawUint32(0))
		}
		if frame.RawUint32(1) != 67890 {
			t.Errorf("RawUint32(1) = %d, want 67890", frame.RawUint32(1))
		}
	}
}

func TestDigital(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 8, Type: device.ChannelTypeDIO},
	}
	dec, _ := NewDecoder(channels, cal)

	chunk := makeFrame(device.ADCMidscale, 0xAB)
	for frame := range dec.Frames(chunk) {
		if frame.Digital() != 0xAB {
			t.Errorf("Digital() = 0x%02x, want 0xAB", frame.Digital())
		}
	}
}

func TestDigital_NoDIO(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec, _ := NewDecoder(channels, cal)

	chunk := makeFrame(device.ADCMidscale)
	for frame := range dec.Frames(chunk) {
		if frame.Digital() != 0 {
			t.Errorf("Digital() with no DIO channel = %d, want 0", frame.Digital())
		}
	}
}

func TestVoltage_WithCalibration(t *testing.T) {
	var cal device.CalibrationTable
	// Apply a known calibration: slope=1.001, offset=5.0
	cal[0][device.BP10V] = device.Calibration{Slope: 1.001, Offset: 5.0}

	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
	}
	dec, _ := NewDecoder(channels, cal)

	// raw=ADCMidscale with slope=1.001 offset=5 → non-zero voltage
	chunk := makeFrame(device.ADCMidscale)
	for frame := range dec.Frames(chunk) {
		got := frame.Voltage(0)
		if math.Abs(got) < 0.001 {
			t.Errorf("expected non-zero voltage with non-identity calibration, got %v", got)
		}
	}
}

func TestCalibration_ToVolts(t *testing.T) {
	cal := device.Calibration{Slope: 1.0, Offset: 0.0}

	tests := []struct {
		name    string
		raw     uint32
		r       device.Range
		want    float64
		epsilon float64
	}{
		{"BP10V midscale", device.ADCMidscale, device.BP10V, 0.0, 0.001},
		{"BP10V zero", 0, device.BP10V, -10.0, 0.001},
		{"UP10V max", device.ADCMaxCode, device.UP10V, 10.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cal.ToVolts(tt.raw, tt.r)
			if math.Abs(got-tt.want) > tt.epsilon {
				t.Errorf("ToVolts(%d, %s) = %v, want ~%v", tt.raw, tt.r, got, tt.want)
			}
		})
	}
}

func FuzzCalibrate(f *testing.F) {
	for _, raw := range []uint32{0, device.ADCMidscale, device.ADCMaxCode, 100000} {
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
		v := cal.ToVolts(raw, device.Range(r))

		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Errorf("ToVolts(%d, %d, {%v, %v}) = %v", raw, r, slope, offset, v)
		}
	})
}

func BenchmarkVoltage(b *testing.B) {
	const numCh = 8
	const numFrames = 1000

	cal := identityCal()
	channels := make([]device.ChannelConfig, numCh)
	for i := range numCh {
		channels[i] = device.ChannelConfig{
			Index: i,
			Type:  device.ChannelTypeAnalog,
			Range: device.BP10V,
			Mode:  device.SingleEnded,
		}
	}
	dec, _ := NewDecoder(channels, cal)

	chunk := make([]byte, numFrames*numCh*4)
	for i := range numFrames * numCh {
		binary.LittleEndian.PutUint32(chunk[i*4:], uint32(i%(device.ADCMaxCode+1)))
	}

	b.ResetTimer()
	b.ReportAllocs()
	var sink float64
	for b.Loop() {
		for frame := range dec.Frames(chunk) {
			for ch := range numCh {
				sink += frame.Voltage(ch)
			}
		}
	}
	_ = sink
}

func BenchmarkToVolts(b *testing.B) {
	cal := device.Calibration{Slope: 1.0, Offset: 0.0}
	b.ResetTimer()
	b.ReportAllocs()
	var sink float64
	for b.Loop() {
		sink += cal.ToVolts(device.ADCMidscale, device.BP10V)
	}
	_ = sink
}

func TestDecodeVoltages(t *testing.T) {
	cal := identityCal()
	channels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP5V},
	}
	dec, _ := NewDecoder(channels, cal)

	// 3 frames, 2 channels.
	chunk := makeFrame(
		device.ADCMidscale, 0,
		0, device.ADCMidscale,
		device.ADCMaxCode, device.ADCMaxCode,
	)

	table := dec.DecodeVoltages(chunk)
	if table.Frames() != 3 {
		t.Fatalf("Frames() = %d, want 3", table.Frames())
	}

	// Frame 0: midscale BP10V → 0V, zero BP5V → -5V.
	row := table.Row(0)
	if math.Abs(row[0]) > 0.001 {
		t.Errorf("frame 0 ch0 = %f, want ~0", row[0])
	}
	if math.Abs(row[1]+5.0) > 0.001 {
		t.Errorf("frame 0 ch1 = %f, want ~-5", row[1])
	}

	// Frame 1: zero BP10V → -10V, midscale BP5V → 0V.
	row = table.Row(1)
	if math.Abs(row[0]+10.0) > 0.001 {
		t.Errorf("frame 1 ch0 = %f, want ~-10", row[0])
	}
	if math.Abs(row[1]) > 0.001 {
		t.Errorf("frame 1 ch1 = %f, want ~0", row[1])
	}
}

func BenchmarkDecodeVoltages(b *testing.B) {
	const numCh = 8
	const numFrames = 1000

	cal := identityCal()
	channels := make([]device.ChannelConfig, numCh)
	for i := range numCh {
		channels[i] = device.ChannelConfig{
			Index: i, Type: device.ChannelTypeAnalog,
			Range: device.BP10V, Mode: device.SingleEnded,
		}
	}
	dec, _ := NewDecoder(channels, cal)

	chunk := make([]byte, numFrames*numCh*4)
	for i := range numFrames * numCh {
		binary.LittleEndian.PutUint32(chunk[i*4:], uint32(i%(device.ADCMaxCode+1)))
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		dec.DecodeVoltages(chunk)
	}
}

func BenchmarkDecodeVoltagesInto(b *testing.B) {
	const numCh = 8
	const numFrames = 1000

	cal := identityCal()
	channels := make([]device.ChannelConfig, numCh)
	for i := range numCh {
		channels[i] = device.ChannelConfig{
			Index: i, Type: device.ChannelTypeAnalog,
			Range: device.BP10V, Mode: device.SingleEnded,
		}
	}
	dec, _ := NewDecoder(channels, cal)

	chunk := make([]byte, numFrames*numCh*4)
	for i := range numFrames * numCh {
		binary.LittleEndian.PutUint32(chunk[i*4:], uint32(i%(device.ADCMaxCode+1)))
	}
	dst := make([]float64, numFrames*numCh)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		dec.DecodeVoltagesInto(chunk, dst)
	}
}

func BenchmarkVoltageOnly(b *testing.B) {
	const numCh = 8
	cal := identityCal()
	channels := make([]device.ChannelConfig, numCh)
	for i := range numCh {
		channels[i] = device.ChannelConfig{
			Index: i, Type: device.ChannelTypeAnalog,
			Range: device.BP10V, Mode: device.SingleEnded,
		}
	}
	dec, _ := NewDecoder(channels, cal)
	data := make([]byte, numCh*4)
	for i := range numCh {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(device.ADCMidscale+i))
	}
	f := &Frame{coeffs: dec.coeffs, channels: dec.channels, data: data}

	b.ResetTimer()
	b.ReportAllocs()
	var sink float64
	for b.Loop() {
		for ch := range numCh {
			sink += f.Voltage(ch)
		}
	}
	_ = sink
}
