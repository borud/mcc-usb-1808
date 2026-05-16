package usb1808

import (
	"bytes"
	"testing"

	"github.com/borud/mcc-usb-1808/v3/internal/wire"
	"github.com/borud/mcc-usb-1808/v3/internal/transport"
)

func TestBlinkLED(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ControlOut response
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.BlinkLED(3); err != nil {
		t.Fatal(err)
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	call := mock.Calls[0]
	if call.Method != "ControlOut" {
		t.Errorf("method = %s, want ControlOut", call.Method)
	}
	if call.Request != cmdBlinkLED {
		t.Errorf("request = 0x%02x, want 0x%02x", call.Request, cmdBlinkLED)
	}
	if !bytes.Equal(call.Data, []byte{3}) {
		t.Errorf("data = %x, want [03]", call.Data)
	}
}

func TestStatus(t *testing.T) {
	statusWord := wire.PutUint16LE(StatusFPGAConfigured | StatusAInScanRunning)
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: statusWord},
	)
	dev := NewDevice(mock, USB1808)

	st, err := dev.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.FPGAConfigured() {
		t.Error("expected FPGAConfigured")
	}
	if !st.AInScanRunning() {
		t.Error("expected AInScanRunning")
	}
	if st.AOutScanRunning() {
		t.Error("did not expect AOutScanRunning")
	}
}

func TestFPGAVersion(t *testing.T) {
	// Version 1.2: high byte=1, low byte=2. As LE uint16: 0x0102.
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: wire.PutUint16LE(0x0102)},
	)
	dev := NewDevice(mock, USB1808)

	major, minor, err := dev.FPGAVersion()
	if err != nil {
		t.Fatal(err)
	}
	if major != 1 || minor != 2 {
		t.Errorf("version = %d.%d, want 1.2", major, minor)
	}
}

func TestSerialNumber(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: []byte("12345678")},
	)
	dev := NewDevice(mock, USB1808)

	sn, err := dev.SerialNumber()
	if err != nil {
		t.Fatal(err)
	}
	if sn != "12345678" {
		t.Errorf("serial = %q, want %q", sn, "12345678")
	}
}

func TestReset(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ControlOut
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.Reset(); err != nil {
		t.Fatal(err)
	}
	if mock.Calls[0].Request != cmdReset {
		t.Errorf("request = 0x%02x, want 0x%02x", mock.Calls[0].Request, cmdReset)
	}
}

func TestSetDigitalDirectionRead(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},                                         // write
		transport.MockResponse{Data: wire.PutUint16LE(0x000F)},      // read
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.SetDigitalDirection(0x0F); err != nil {
		t.Fatal(err)
	}

	val, err := dev.DigitalDirection()
	if err != nil {
		t.Fatal(err)
	}
	if val != 0x0F {
		t.Errorf("tristate = 0x%04x, want 0x000F", val)
	}

	// Verify write call.
	writeCall := mock.Calls[0]
	if writeCall.Request != cmdDTristate {
		t.Errorf("request = 0x%02x, want 0x%02x", writeCall.Request, cmdDTristate)
	}
	if writeCall.WValue != 0x0F {
		t.Errorf("wValue = 0x%04x, want 0x000F", writeCall.WValue)
	}
}

func TestReadDigital(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: []byte{0x05}},
	)
	dev := NewDevice(mock, USB1808)

	val, err := dev.ReadDigital()
	if err != nil {
		t.Fatal(err)
	}
	if val != 0x05 {
		t.Errorf("port = 0x%02x, want 0x05", val)
	}
}

func TestWriteDigitalRead(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},                    // write
		transport.MockResponse{Data: []byte{0x0A}},  // read
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.WriteDigital(0x0A); err != nil {
		t.Fatal(err)
	}
	val, err := dev.DigitalLatch()
	if err != nil {
		t.Fatal(err)
	}
	if val != 0x0A {
		t.Errorf("latch = 0x%02x, want 0x0A", val)
	}
}

func TestConfigureAnalogIn(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ControlOut
	)
	dev := NewDevice(mock, USB1808)

	configs := []AnalogInChannelConfig{
		{Channel: 0, Range: BP10V, Mode: Differential},
		{Channel: 1, Range: BP5V, Mode: SingleEnded},
		{Channel: 2, Range: UP10V, Mode: Differential},
		{Channel: 3, Range: UP5V, Mode: Grounded},
	}
	if err := dev.ConfigureAnalogIn(configs); err != nil {
		t.Fatal(err)
	}

	call := mock.Calls[0]
	if call.Request != cmdADCSetup {
		t.Errorf("request = 0x%02x, want 0x%02x", call.Request, cmdADCSetup)
	}
	// ch0: BP10V|Diff = 0x00, ch1: BP5V|(SE<<2) = 0x05, ch2: UP10V = 0x02, ch3: UP5V|(Grounded<<2) = 0x0F
	want := []byte{0x00, 0x05, 0x02, 0x0F, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(call.Data, want) {
		t.Errorf("data = %x, want %x", call.Data, want)
	}
}

func TestAnalogInRaw(t *testing.T) {
	// 8 channels, each with a known value.
	data := make([]byte, 32)
	for i := range 8 {
		copy(data[i*4:], wire.PutUint32LE(uint32(i*1000)))
	}
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: data},
	)
	dev := NewDevice(mock, USB1808)

	vals, err := dev.AnalogInRaw()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		if vals[i] != uint32(i*1000) {
			t.Errorf("channel %d = %d, want %d", i, vals[i], i*1000)
		}
	}
}

func TestConfigureAnalogInScan(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ControlOut
	)
	dev := NewDevice(mock, USB1808)

	channels := []int{0, 1, 2, 3}
	if err := dev.ConfigureAnalogInScan(channels); err != nil {
		t.Fatal(err)
	}

	call := mock.Calls[0]
	if call.Request != cmdAInConfig {
		t.Errorf("request = 0x%02x, want 0x%02x", call.Request, cmdAInConfig)
	}
	// wIndex = lastChan - 1 = 3
	if call.WIndex != 3 {
		t.Errorf("wIndex = %d, want 3", call.WIndex)
	}
}

func TestStartAnalogInScan(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ControlOut
	)
	dev := NewDevice(mock, USB1808)

	cfg := AnalogInScanConfig{
		Channels: []int{0, 1, 2, 3},
		Rate:     100000,
		Count:    1000,
	}
	if err := dev.StartAnalogInScan(cfg); err != nil {
		t.Fatal(err)
	}

	call := mock.Calls[0]
	if call.Request != cmdAInScanStart {
		t.Errorf("request = 0x%02x, want 0x%02x", call.Request, cmdAInScanStart)
	}
	// Golden byte verification.
	want := []byte{
		0xE8, 0x03, 0x00, 0x00, // scan_count = 1000
		0x00, 0x00, 0x00, 0x00, // retrig = 0
		0xE7, 0x03, 0x00, 0x00, // pacer_period = 999
		0xFF,                   // packet_size = 255
		0x00,                   // options = 0
	}
	if !bytes.Equal(call.Data, want) {
		t.Errorf("payload mismatch:\ngot:  %x\nwant: %x", call.Data, want)
	}
}

func TestTriggerConfigWriteRead(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},                    // write
		transport.MockResponse{Data: []byte{0x03}},  // read: edge + high
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.SetTriggerConfig(TriggerEdge | TriggerHigh); err != nil {
		t.Fatal(err)
	}
	val, err := dev.TriggerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if val != 0x03 {
		t.Errorf("trigger config = 0x%02x, want 0x03", val)
	}
}

func TestPatternDetect(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},                              // write
		transport.MockResponse{Data: []byte{0x05, 0x0F, 0x00}}, // read
	)
	dev := NewDevice(mock, USB1808)

	cfg := PatternDetectConfig{Value: 0x05, Mask: 0x0F, Options: PatternEqual}
	if err := dev.SetPatternDetect(cfg); err != nil {
		t.Fatal(err)
	}

	got, err := dev.PatternDetect()
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != 0x05 || got.Mask != 0x0F || got.Options != 0x00 {
		t.Errorf("pattern = %+v, want Value=0x05 Mask=0x0F Options=0x00", got)
	}
}

func TestCounterReadWrite(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},                                             // write
		transport.MockResponse{Data: wire.PutUint32LE(42)},               // read
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.WriteCounter(0, 42); err != nil {
		t.Fatal(err)
	}
	val, err := dev.ReadCounter(0)
	if err != nil {
		t.Fatal(err)
	}
	if val != 42 {
		t.Errorf("counter = %d, want 42", val)
	}
}

func TestSetTimerParams(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ControlOut for SetTimerParams
	)
	dev := NewDevice(mock, USB1808)

	cfg := TimerConfig{Frequency: 1000, DutyCycle: 0.5}
	if err := dev.SetTimerParams(0, cfg); err != nil {
		t.Fatal(err)
	}

	call := mock.Calls[0]
	if call.Request != cmdTimerParams {
		t.Errorf("request = 0x%02x, want 0x%02x", call.Request, cmdTimerParams)
	}

	// Verify period and pulse width in payload.
	period := wire.Uint32LE(call.Data[0:4])
	pw := wire.Uint32LE(call.Data[4:8])
	if period != 99999 {
		t.Errorf("period = %d, want 99999", period)
	}
	if pw != 49999 {
		t.Errorf("pulseWidth = %d, want 49999", pw)
	}

	// Verify cached.
	cached, _ := dev.TimerParams(0)
	if cached.Frequency != 1000 {
		t.Errorf("cached frequency = %v, want 1000", cached.Frequency)
	}
}

func TestAnalogInToVolts(t *testing.T) {
	dev := NewDevice(nil, USB1808)
	// Set identity calibration (slope=1, offset=0).
	for ch := range NumAInChannels {
		for gain := range NumAInRanges {
			dev.calAIn[ch][gain] = Calibration{Slope: 1.0, Offset: 0.0}
		}
	}

	tests := []struct {
		raw     uint32
		r       Range
		want    float64
		epsilon float64
	}{
		{131072, BP10V, 0.0, 0.001},
		{0, BP10V, -10.0, 0.001},
		{262143, BP10V, 9.99992, 0.001},
		{0, UP10V, 0.0, 0.001},
		{262143, UP10V, 10.0, 0.001},
		{131072, BP5V, 0.0, 0.001},
		{0, UP5V, 0.0, 0.001},
	}
	for _, tt := range tests {
		got := dev.AnalogInToVolts(tt.raw, 0, tt.r)
		diff := got - tt.want
		if diff < -tt.epsilon || diff > tt.epsilon {
			t.Errorf("AInToVolts(%d, %s) = %v, want ~%v", tt.raw, tt.r, got, tt.want)
		}
	}
}

func TestVoltsToAnalogOut(t *testing.T) {
	dev := NewDevice(nil, USB1808)
	// Identity calibration.
	for ch := range NumAOutChannels {
		dev.calAOut[ch] = Calibration{Slope: 1.0, Offset: 0.0}
	}

	tests := []struct {
		volts float64
		want  uint16
	}{
		{0.0, 32768},
		{-10.0, 0},
		{10.0, 65535},
	}
	for _, tt := range tests {
		got := dev.VoltsToAnalogOut(tt.volts, 0)
		if got != tt.want {
			t.Errorf("VoltsToAOut(%v) = %d, want %d", tt.volts, got, tt.want)
		}
	}
}

func TestModelString(t *testing.T) {
	if USB1808.String() != "USB-1808" {
		t.Errorf("USB1808.String() = %s", USB1808.String())
	}
	if USB1808X.String() != "USB-1808X" {
		t.Errorf("USB1808X.String() = %s", USB1808X.String())
	}
}

func TestClose(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // AInScanStop
		transport.MockResponse{}, // AOutScanStop
	)
	dev := NewDevice(mock, USB1808)
	dev.initialized.Store(true)

	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}
	if !mock.Closed() {
		t.Error("expected transport to be closed")
	}
}
