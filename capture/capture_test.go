package capture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"testing"
	"time"
)

func testHeaderRaw(channels int) Header {
	chs := make([]Channel, channels)
	for i := range chs {
		chs[i] = Channel{
			Index: i,
			Type:  AnalogIn,
			Range: 0, // BP10V
			Cal:   &CalEntry{Slope: 1.0, Offset: 0.0},
		}
	}
	return Header{
		DeviceModel:     "USB-1808",
		DeviceSerial:    "12345678",
		FPGAVersion:     "1.5",
		CalibrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		Channels:        chs,
		SampleRate:      1000,
		Format:          RawUint32,
		Timestamp:       time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func testHeaderMixed() Header {
	return Header{
		DeviceModel:     "USB-1808X",
		DeviceSerial:    "87654321",
		FPGAVersion:     "2.0",
		CalibrationDate: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Channels: []Channel{
			{Index: 0, Type: AnalogIn, Range: 0, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}},
			{Index: 1, Type: AnalogIn, Range: 1, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}},
			{Index: 8, Type: DigitalIO},
			{Index: 9, Type: Counter},
			{Index: 11, Type: Encoder},
		},
		SampleRate:      10000,
		Format:          RawUint32,
		Timestamp:       time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC).UnixMilli(),
		ApplicationName: "daq-capture",
		SessionID:       "run-042",
		Description:     "Motor vibration test under load",
		Operator:        "testbot",
		Properties:      map[string]string{"ambient_temp": "22.5C", "dut_serial": "SN-999"},
	}
}

// writeRawFrames writes n frames of raw data, returning the values written.
func writeRawFrames(t *testing.T, w *Writer, numCh, n int) [][]uint32 {
	t.Helper()
	frames := make([][]uint32, n)
	for i := range n {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			vals[ch] = uint32(i*numCh+ch) & 0x3FFFF // 18-bit range
		}
		frames[i] = vals
		if err := w.WriteFrame(vals); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}
	return frames
}


// TestNewWriter_NoChannels verifies that NewWriter returns ErrNoChannels for an empty channel list.
func TestNewWriter_NoChannels(t *testing.T) {
	h := Header{Format: RawUint32}
	_, err := NewWriter(io.Discard, h)
	if !errors.Is(err, ErrNoChannels) {
		t.Fatalf("expected ErrNoChannels, got %v", err)
	}
}

func TestNewWriter_InvalidFormat(t *testing.T) {
	h := testHeaderRaw(2)
	h.Format = DataFormat(99)
	_, err := NewWriter(io.Discard, h)
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got %v", err)
	}
}


func TestWriteFrame_SizeMismatch(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(3))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := w.WriteFrame([]uint32{1, 2}); !errors.Is(err, ErrFrameSizeMismatch) {
		t.Fatalf("expected ErrFrameSizeMismatch, got %v", err)
	}
	if err := w.WriteFrame([]uint32{1, 2, 3, 4}); !errors.Is(err, ErrFrameSizeMismatch) {
		t.Fatalf("expected ErrFrameSizeMismatch, got %v", err)
	}
}


func TestWriteFrame_AfterClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	if err := w.WriteFrame([]uint32{1, 2}); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("expected ErrWriterClosed, got %v", err)
	}
}


func TestFlush_AfterClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	if err := w.Flush(); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("expected ErrWriterClosed, got %v", err)
	}
}

func TestClose_DoubleClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("expected ErrWriterClosed on double close, got %v", err)
	}
}

// TestNewReader_InvalidMagic verifies that NewReader rejects files with invalid magic bytes.
func TestNewReader_InvalidMagic(t *testing.T) {
	data := make([]byte, preambleLen)
	copy(data, "NOTD")
	_, err := NewReader(bytes.NewReader(data))
	if !errors.Is(err, ErrInvalidMagic) {
		t.Fatalf("expected ErrInvalidMagic, got %v", err)
	}
}

func TestNewReader_UnsupportedVersion(t *testing.T) {
	data := make([]byte, preambleLen)
	copy(data, fileMagic)
	data[4] = 99 // bad version
	_, err := NewReader(bytes.NewReader(data))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestNewReader_EmptyReader(t *testing.T) {
	_, err := NewReader(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error on empty reader")
	}
}

func TestNewReader_TruncatedHeader(t *testing.T) {
	data := make([]byte, preambleLen)
	copy(data, fileMagic)
	data[4] = fileVersion
	binary.LittleEndian.PutUint32(data[6:], 100) // claims 100 bytes of header
	// but only preamble is provided
	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Fatal("expected error on truncated header")
	}
}

func TestReadFrame_AfterClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{1, 2})
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()

	_, err = r.ReadFrame()
	if !errors.Is(err, ErrReaderClosed) {
		t.Fatalf("expected ErrReaderClosed, got %v", err)
	}
}

func TestReaderClose_DoubleClose(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); !errors.Is(err, ErrReaderClosed) {
		t.Fatalf("expected ErrReaderClosed on double close, got %v", err)
	}
}

// TestRoundTrip_RawUint32 writes and reads back RawUint32 frames, verifying header and data integrity.
func TestRoundTrip_RawUint32(t *testing.T) {
	const numCh = 4
	const numFrames = 100

	var buf bytes.Buffer
	h := testHeaderRaw(numCh)
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Verify header round-trips.
	rh := r.Header()
	if rh.DeviceModel != h.DeviceModel {
		t.Errorf("model = %q, want %q", rh.DeviceModel, h.DeviceModel)
	}
	if rh.Format != RawUint32 {
		t.Errorf("format = %d, want RawUint32", rh.Format)
	}
	if len(rh.Channels) != numCh {
		t.Fatalf("channels = %d, want %d", len(rh.Channels), numCh)
	}

	for i := range numFrames {
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		raw := f.RawValues()
		if len(raw) != numCh {
			t.Fatalf("frame %d: got %d values, want %d", i, len(raw), numCh)
		}
		for ch := range numCh {
			if raw[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", i, ch, raw[ch], written[i][ch])
			}
		}
	}

	// Next read should be EOF.
	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after all frames, got %v", err)
	}
}


// TestRoundTrip_RawCompressed verifies round-trip of compressed RawUint32 frames.
func TestRoundTrip_RawCompressed(t *testing.T) {
	const numCh = 4
	const numFrames = 500

	var buf bytes.Buffer
	h := testHeaderRaw(numCh)
	w, err := NewWriter(&buf, h, WithCompression(true))
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := range numFrames {
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		raw := f.RawValues()
		for ch := range numCh {
			if raw[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", i, ch, raw[ch], written[i][ch])
			}
		}
	}

	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}


// TestRoundTrip_SingleFrame verifies round-trip of a single frame followed by EOF.
func TestRoundTrip_SingleFrame(t *testing.T) {
	var buf bytes.Buffer
	h := testHeaderRaw(2)
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{0xAAAA, 0xBBBB})
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f, err := r.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	raw := f.RawValues()
	if raw[0] != 0xAAAA || raw[1] != 0xBBBB {
		t.Errorf("got [%x, %x], want [aaaa, bbbb]", raw[0], raw[1])
	}

	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// TestRoundTrip_ZeroFrames verifies that a header-only capture reads back as immediate EOF.
func TestRoundTrip_ZeroFrames(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF on empty file, got %v", err)
	}
}

// TestRoundTrip_ManyChannels verifies round-trip with 13 channels (max scan queue).
func TestRoundTrip_ManyChannels(t *testing.T) {
	const numCh = 13 // max scan queue
	const numFrames = 10

	var buf bytes.Buffer
	h := testHeaderRaw(numCh)
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := range numFrames {
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		for ch := range numCh {
			if f.RawValues()[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d mismatch", i, ch)
			}
		}
	}
}

// TestCalibrate_BP10V verifies calibration for the +/-10V bipolar range.
func TestCalibrate_BP10V(t *testing.T) {
	ch := Channel{Type: AnalogIn, Range: 0, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}}

	// Midpoint → 0V
	got := calibrate(131072, ch)
	if got != 0.0 {
		t.Errorf("midpoint: got %f, want 0.0", got)
	}

	// Zero → -10V
	got = calibrate(0, ch)
	if got != -10.0 {
		t.Errorf("zero: got %f, want -10.0", got)
	}

	// Max → ~10V
	got = calibrate(0x3FFFF, ch) // 262143
	want := (262143.0 - 131072.0) * 10.0 / 131072.0
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("max: got %f, want %f", got, want)
	}
}

func TestCalibrate_BP5V(t *testing.T) {
	ch := Channel{Type: AnalogIn, Range: 1, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}}

	got := calibrate(131072, ch)
	if got != 0.0 {
		t.Errorf("midpoint: got %f, want 0.0", got)
	}

	got = calibrate(0, ch)
	if got != -5.0 {
		t.Errorf("zero: got %f, want -5.0", got)
	}
}

func TestCalibrate_UP10V(t *testing.T) {
	ch := Channel{Type: AnalogIn, Range: 2, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}}

	got := calibrate(0, ch)
	if got != 0.0 {
		t.Errorf("zero: got %f, want 0.0", got)
	}

	got = calibrate(0x3FFFF, ch)
	if math.Abs(got-10.0) > 1e-6 {
		t.Errorf("max: got %f, want 10.0", got)
	}
}

func TestCalibrate_UP5V(t *testing.T) {
	ch := Channel{Type: AnalogIn, Range: 3, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}}

	got := calibrate(0x3FFFF, ch)
	if math.Abs(got-5.0) > 1e-6 {
		t.Errorf("max: got %f, want 5.0", got)
	}
}

func TestCalibrate_WithSlopeOffset(t *testing.T) {
	// Non-unity calibration.
	ch := Channel{Type: AnalogIn, Range: 0, Cal: &CalEntry{Slope: 1.0005, Offset: 10.0}}

	raw := uint32(131072)
	cal := float64(raw)*float64(ch.Cal.Slope) + float64(ch.Cal.Offset)
	cal = math.Round(cal)
	want := (cal - 131072.0) * 10.0 / 131072.0

	got := calibrate(raw, ch)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("got %f, want %f", got, want)
	}
}

func TestCalibrate_NonAnalogPassthrough(t *testing.T) {
	for _, ct := range []ChannelType{DigitalIO, Counter, Encoder} {
		ch := Channel{Type: ct}
		raw := uint32(42)
		got := calibrate(raw, ch)
		if got != 42.0 {
			t.Errorf("type %d: got %f, want 42.0", ct, got)
		}
	}
}

func TestCalibrate_NilCal(t *testing.T) {
	ch := Channel{Type: AnalogIn, Range: 0, Cal: nil}
	got := calibrate(1000, ch)
	if got != 1000.0 {
		t.Errorf("nil cal: got %f, want 1000.0", got)
	}
}

func TestCalibrate_UnipolarClamp(t *testing.T) {
	// Offset that would push calibrated value negative for unipolar range.
	ch := Channel{Type: AnalogIn, Range: 2, Cal: &CalEntry{Slope: 1.0, Offset: -1000.0}}
	got := calibrate(100, ch)
	// cal = 100 - 1000 = -900 → clamped to 0 → 0 * 10 / 262143 = 0V
	if got != 0.0 {
		t.Errorf("unipolar clamp low: got %f, want 0.0", got)
	}
}

// TestFrame_Values_RawWithCalibration verifies end-to-end calibrated Values() for raw frames with mixed channel types.
func TestFrame_Values_RawWithCalibration(t *testing.T) {
	h := testHeaderMixed()

	var buf bytes.Buffer
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}

	// Write a frame: analog0=131072 (0V BP10V), analog1=131072 (0V BP5V), digital=0xFF, counter=1000, encoder=500
	frame := []uint32{131072, 131072, 0xFF, 1000, 500}
	w.WriteFrame(frame)
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f, err := r.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}

	vals := f.Values()
	if len(vals) != 5 {
		t.Fatalf("got %d values, want 5", len(vals))
	}

	// Channel 0: BP10V midpoint → 0V
	if vals[0] != 0.0 {
		t.Errorf("ch0 (BP10V midpoint): got %f, want 0.0", vals[0])
	}
	// Channel 1: BP5V midpoint → 0V
	if vals[1] != 0.0 {
		t.Errorf("ch1 (BP5V midpoint): got %f, want 0.0", vals[1])
	}
	// Channel 2: DigitalIO → passthrough
	if vals[2] != 255.0 {
		t.Errorf("ch2 (digital): got %f, want 255.0", vals[2])
	}
	// Channel 3: Counter → passthrough
	if vals[3] != 1000.0 {
		t.Errorf("ch3 (counter): got %f, want 1000.0", vals[3])
	}
	// Channel 4: Encoder → passthrough
	if vals[4] != 500.0 {
		t.Errorf("ch4 (encoder): got %f, want 500.0", vals[4])
	}
}


// TestFrames_Iterator verifies the Frames() range iterator reads all frames in order.
func TestFrames_Iterator(t *testing.T) {
	const numCh = 2
	const numFrames = 10

	var buf bytes.Buffer
	h := testHeaderRaw(numCh)
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	count := 0
	for f, err := range r.Frames() {
		if err != nil {
			t.Fatalf("frame %d: %v", count, err)
		}
		raw := f.RawValues()
		for ch := range numCh {
			if raw[ch] != written[count][ch] {
				t.Errorf("frame %d ch %d mismatch", count, ch)
			}
		}
		count++
	}
	if count != numFrames {
		t.Errorf("iterated %d frames, want %d", count, numFrames)
	}
}

func TestFrames_Iterator_EarlyBreak(t *testing.T) {
	const numCh = 2
	const numFrames = 100

	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(numCh))
	if err != nil {
		t.Fatal(err)
	}
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	count := 0
	for _, err := range r.Frames() {
		if err != nil {
			t.Fatal(err)
		}
		count++
		if count == 5 {
			break
		}
	}
	if count != 5 {
		t.Errorf("iterated %d frames, want 5", count)
	}
}

// TestWithBufferSize verifies correct round-trip with a small buffer that forces multiple flushes.
func TestWithBufferSize(t *testing.T) {
	// Use a tiny buffer (2 frames) to force multiple flushes.
	const numCh = 2
	const numFrames = 10

	var buf bytes.Buffer
	h := testHeaderRaw(numCh)
	w, err := NewWriter(&buf, h, WithBufferSize(2))
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := range numFrames {
		f, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		for ch := range numCh {
			if f.RawValues()[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d mismatch", i, ch)
			}
		}
	}
}

func TestClose_FlushesPartialBuffer(t *testing.T) {
	// Write 3 frames with a buffer of 100. Close should flush the partial buffer.
	const numCh = 2
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(numCh), WithBufferSize(100))
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{1, 2})
	w.WriteFrame([]uint32{3, 4})
	w.WriteFrame([]uint32{5, 6})
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := range 3 {
		_, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
	}
	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// TestReadFrame_TruncatedData verifies that reading a truncated frame returns io.ErrUnexpectedEOF.
func TestReadFrame_TruncatedData(t *testing.T) {
	var buf bytes.Buffer
	h := testHeaderRaw(4) // 4 channels = 16 bytes per frame
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{1, 2, 3, 4})
	w.Close()

	// Truncate the data (remove last few bytes of frame data).
	data := buf.Bytes()
	truncated := data[:len(data)-4]

	r, err := NewReader(bytes.NewReader(truncated))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.ReadFrame()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF on truncated frame, got %v", err)
	}
}

// TestHeader_RoundTrip verifies that all header fields survive a write/read round-trip.
func TestHeader_RoundTrip(t *testing.T) {
	h := testHeaderMixed()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	rh := r.Header()

	// Device identification.
	if rh.DeviceModel != h.DeviceModel {
		t.Errorf("DeviceModel = %q, want %q", rh.DeviceModel, h.DeviceModel)
	}
	if rh.DeviceSerial != h.DeviceSerial {
		t.Errorf("DeviceSerial = %q, want %q", rh.DeviceSerial, h.DeviceSerial)
	}
	if rh.FPGAVersion != h.FPGAVersion {
		t.Errorf("FPGAVersion = %q, want %q", rh.FPGAVersion, h.FPGAVersion)
	}
	if !rh.CalibrationDate.Equal(h.CalibrationDate) {
		t.Errorf("CalibrationDate = %v, want %v", rh.CalibrationDate, h.CalibrationDate)
	}

	// Capture configuration.
	if rh.SampleRate != h.SampleRate {
		t.Errorf("SampleRate = %f, want %f", rh.SampleRate, h.SampleRate)
	}
	if rh.Format != h.Format {
		t.Errorf("Format = %d, want %d", rh.Format, h.Format)
	}
	if rh.Timestamp != h.Timestamp {
		t.Errorf("Timestamp = %d, want %d", rh.Timestamp, h.Timestamp)
	}

	// Optional session metadata.
	if rh.ApplicationName != h.ApplicationName {
		t.Errorf("ApplicationName = %q, want %q", rh.ApplicationName, h.ApplicationName)
	}
	if rh.SessionID != h.SessionID {
		t.Errorf("SessionID = %q, want %q", rh.SessionID, h.SessionID)
	}
	if rh.Description != h.Description {
		t.Errorf("Description = %q, want %q", rh.Description, h.Description)
	}
	if rh.Operator != h.Operator {
		t.Errorf("Operator = %q, want %q", rh.Operator, h.Operator)
	}

	// Properties.
	if len(rh.Properties) != len(h.Properties) {
		t.Fatalf("Properties len = %d, want %d", len(rh.Properties), len(h.Properties))
	}
	for k, v := range h.Properties {
		if rh.Properties[k] != v {
			t.Errorf("Properties[%q] = %q, want %q", k, rh.Properties[k], v)
		}
	}

	// Channels.
	if len(rh.Channels) != len(h.Channels) {
		t.Fatalf("Channels len = %d, want %d", len(rh.Channels), len(h.Channels))
	}
	for i, ch := range rh.Channels {
		want := h.Channels[i]
		if ch.Index != want.Index || ch.Type != want.Type || ch.Range != want.Range {
			t.Errorf("channel %d: got {%d,%d,%d}, want {%d,%d,%d}",
				i, ch.Index, ch.Type, ch.Range, want.Index, want.Type, want.Range)
		}
		if want.Cal != nil {
			if ch.Cal == nil {
				t.Errorf("channel %d: cal is nil, want non-nil", i)
			} else if ch.Cal.Slope != want.Cal.Slope || ch.Cal.Offset != want.Cal.Offset {
				t.Errorf("channel %d: cal = {%f,%f}, want {%f,%f}",
					i, ch.Cal.Slope, ch.Cal.Offset, want.Cal.Slope, want.Cal.Offset)
			}
		}
	}
}

// TestCompression_ReducesSize verifies that compressed output is smaller than uncompressed for a slowly-varying signal.
func TestCompression_ReducesSize(t *testing.T) {
	const numCh = 4
	const numFrames = 10000

	h := testHeaderRaw(numCh)

	var uncompressed bytes.Buffer
	w, err := NewWriter(&uncompressed, h)
	if err != nil {
		t.Fatal(err)
	}
	for i := range numFrames {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			// Slowly varying signal — compresses well.
			vals[ch] = uint32(131072 + (i % 100))
		}
		w.WriteFrame(vals)
	}
	w.Close()

	var compressed bytes.Buffer
	w, err = NewWriter(&compressed, h, WithCompression(true))
	if err != nil {
		t.Fatal(err)
	}
	for i := range numFrames {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			vals[ch] = uint32(131072 + (i % 100))
		}
		w.WriteFrame(vals)
	}
	w.Close()

	if compressed.Len() >= uncompressed.Len() {
		t.Errorf("compressed (%d) should be smaller than uncompressed (%d)",
			compressed.Len(), uncompressed.Len())
	}
	t.Logf("uncompressed: %d bytes, compressed: %d bytes (%.1f%% reduction)",
		uncompressed.Len(), compressed.Len(),
		100*(1-float64(compressed.Len())/float64(uncompressed.Len())))
}

// TestFramesWritten verifies the FramesWritten counter before, during, and after writes.
func TestFramesWritten(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}

	if w.FramesWritten() != 0 {
		t.Errorf("before writes: got %d, want 0", w.FramesWritten())
	}

	w.WriteFrame([]uint32{1, 2})
	w.WriteFrame([]uint32{3, 4})
	w.WriteFrame([]uint32{5, 6})

	if w.FramesWritten() != 3 {
		t.Errorf("after 3 writes: got %d, want 3", w.FramesWritten())
	}

	w.Close()

	// Still accessible after Close.
	if w.FramesWritten() != 3 {
		t.Errorf("after close: got %d, want 3", w.FramesWritten())
	}
}

func TestFrameCount_Seekable(t *testing.T) {
	f, err := os.CreateTemp("", "capture-test-*.daq")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	h := testHeaderRaw(2)
	w, err := NewWriter(f, h)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{1, 2})
	w.WriteFrame([]uint32{3, 4})
	w.WriteFrame([]uint32{5, 6})
	w.Close()

	// Re-open and verify frame count was patched.
	rf, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()

	r, err := NewReader(rf)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Header().FrameCount != 3 {
		t.Errorf("FrameCount = %d, want 3", r.Header().FrameCount)
	}
}

func TestFrameCount_NonSeekable(t *testing.T) {
	// bytes.Buffer is not an io.WriteSeeker, so frame count stays 0.
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{1, 2})
	w.WriteFrame([]uint32{3, 4})
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Header().FrameCount != 0 {
		t.Errorf("FrameCount = %d, want 0 for non-seekable writer", r.Header().FrameCount)
	}
}

// TestFrame_Reuse verifies that ReadFrame reuses the internal slice, so values change between calls.
func TestFrame_Reuse(t *testing.T) {
	const numCh = 2
	var buf bytes.Buffer
	w, err := NewWriter(&buf, testHeaderRaw(numCh))
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{100, 200})
	w.WriteFrame([]uint32{300, 400})
	w.Close()

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f1, _ := r.ReadFrame()
	raw1Ptr := &f1.RawValues()[0]

	f2, _ := r.ReadFrame()
	raw2Ptr := &f2.RawValues()[0]

	// Same pointer — frame is reused.
	if raw1Ptr != raw2Ptr {
		t.Error("expected ReadFrame to reuse internal slice")
	}
	// Values reflect second frame.
	if f2.RawValues()[0] != 300 || f2.RawValues()[1] != 400 {
		t.Errorf("second frame: got [%d, %d], want [300, 400]",
			f2.RawValues()[0], f2.RawValues()[1])
	}
}

// channelTypes maps hardware indices to channel types, mirroring queueNames
// in cmd/daq/helpers.go.
var channelTypes = [13]ChannelType{
	AnalogIn, AnalogIn, AnalogIn, AnalogIn, // 0-3
	AnalogIn, AnalogIn, AnalogIn, AnalogIn, // 4-7
	DigitalIO,          // 8
	Counter, Counter,   // 9-10
	Encoder, Encoder,   // 11-12
}

// buildFuzzHeader constructs a Header the same way cmd_capture.go:buildHeader
// does: channel descriptors with types, ranges, and calibration entries based
// on the queue composition.
func buildFuzzHeader(queue []int, ranges []uint8, format DataFormat, rate float64) Header {
	channels := make([]Channel, len(queue))
	analogIdx := 0
	for i, idx := range queue {
		ch := Channel{
			Index: idx,
			Type:  channelTypes[idx],
		}
		if idx < 8 { // analog
			r := ranges[analogIdx]
			ch.Range = r
			ch.Cal = &CalEntry{
				Slope:  1.0 + float32(idx)*0.0001,
				Offset: float32(idx) * 0.5,
			}
			analogIdx++
		}
		channels[i] = ch
	}
	return Header{
		DeviceModel:  "USB-1808X",
		DeviceSerial: "00000001",
		FPGAVersion:  "2.0",
		Channels:     channels,
		SampleRate:   rate,
		Format:       format,
		Timestamp:    1700000000000,
		SessionID:    "fuzz-session",
	}
}

// FuzzCaptureRaw emulates "daq capture --raw" end-to-end: builds a header
// with CLI-like channel/range configuration, writes raw uint32 frames with
// varied compression and buffer sizes, reads them back, and verifies both
// raw round-trip and calibrated Values().
func FuzzCaptureRaw(f *testing.F) {
	// Seed with representative CLI invocations.
	// queueBits encodes which of the 13 channels are in the queue.
	for _, queueBits := range []uint16{
		0x000F, // ain0-3 (--channels 0-3)
		0x00FF, // ain0-7 (--channels 0-7)
		0x0003, // ain0,ain1
		0x0103, // ain0,ain1,dio
		0x1F03, // ain0,ain1,dio,counter0,counter1,encoder0,encoder1
		0x0001, // single channel
		0x1FFF, // all 13
	} {
		for _, rangeBits := range []uint8{0, 1, 2, 3} {
			for _, nFrames := range []int{0, 1, 10, 100} {
				for _, compress := range []bool{false, true} {
					for _, bufSize := range []int{1, 64, 1024} {
						f.Add(queueBits, rangeBits, nFrames, compress, bufSize)
					}
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, queueBits uint16, rangeBits uint8, nFrames int, compress bool, bufSize int) {
		if nFrames < 0 || nFrames > 5000 || bufSize < 1 || bufSize > 100000 {
			t.Skip()
		}
		queueBits &= 0x1FFF // 13 valid channels
		if queueBits == 0 {
			t.Skip()
		}

		// Build queue from bits (same as parseQueue/parseChannels).
		var queue []int
		for i := range 13 {
			if queueBits&(1<<i) != 0 {
				queue = append(queue, i)
			}
		}

		// Assign ranges to analog channels.
		var ranges []uint8
		for _, idx := range queue {
			if idx < 8 {
				ranges = append(ranges, rangeBits%4)
			}
		}

		h := buildFuzzHeader(queue, ranges, RawUint32, 10000)
		nCh := len(queue)

		var buf bytes.Buffer
		var opts []WriterOption
		if compress {
			opts = append(opts, WithCompression(true))
		}
		opts = append(opts, WithBufferSize(bufSize))

		w, err := NewWriter(&buf, h, opts...)
		if err != nil {
			t.Fatal(err)
		}

		// Write frames like the capture loop does.
		for i := range nFrames {
			vals := make([]uint32, nCh)
			for j, idx := range queue {
				switch {
				case idx < 8: // analog: simulated ADC values
					vals[j] = uint32(131072+i*13+j*7) & 0x3FFFF
				case idx == 8: // digital
					vals[j] = uint32(i) & 0xFF
				default: // counter/encoder
					vals[j] = uint32(i * (idx + 1))
				}
			}
			if err := w.WriteFrame(vals); err != nil {
				t.Fatalf("WriteFrame %d: %v", i, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		if w.FramesWritten() != uint64(nFrames) {
			t.Fatalf("FramesWritten = %d, want %d", w.FramesWritten(), nFrames)
		}

		// Read back (emulates "daq file info" + "daq file export" path).
		r, err := NewReader(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()

		rh := r.Header()
		if len(rh.Channels) != nCh {
			t.Fatalf("channels = %d, want %d", len(rh.Channels), nCh)
		}
		if rh.SampleRate != 10000 {
			t.Fatalf("SampleRate = %f, want 10000", rh.SampleRate)
		}

		count := 0
		for frame, err := range r.Frames() {
			if err != nil {
				t.Fatalf("Frames() at %d: %v", count, err)
			}

			// Verify raw values round-trip.
			raw := frame.RawValues()
			if len(raw) != nCh {
				t.Fatalf("frame %d: %d values, want %d", count, len(raw), nCh)
			}
			for j, idx := range queue {
				var want uint32
				switch {
				case idx < 8:
					want = uint32(131072+count*13+j*7) & 0x3FFFF
				case idx == 8:
					want = uint32(count) & 0xFF
				default:
					want = uint32(count * (idx + 1))
				}
				if raw[j] != want {
					t.Fatalf("frame %d ch %d (hw%d): got %d, want %d", count, j, idx, raw[j], want)
				}
			}

			// Verify Values() produces finite numbers (exercises calibrate).
			vals := frame.Values()
			for j, v := range vals {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("frame %d ch %d: Values() = %f", count, j, v)
				}
			}
			count++
		}
		if count != nFrames {
			t.Fatalf("read %d frames, want %d", count, nFrames)
		}
	})
}


// FuzzCalibrate exercises the calibrate function across all ranges with
// fuzzed raw values and calibration coefficients.
func FuzzCalibrate(f *testing.F) {
	for _, rng := range []uint8{0, 1, 2, 3} {
		for _, raw := range []uint32{0, 1, 131072, 262143} {
			f.Add(raw, rng, float32(1.0), float32(0.0))
			f.Add(raw, rng, float32(1.001), float32(5.0))
		}
	}

	f.Fuzz(func(t *testing.T, raw uint32, rng uint8, slope, offset float32) {
		if rng > 3 {
			t.Skip()
		}
		if math.IsNaN(float64(slope)) || math.IsInf(float64(slope), 0) {
			t.Skip()
		}
		if math.IsNaN(float64(offset)) || math.IsInf(float64(offset), 0) {
			t.Skip()
		}

		raw &= 0x3FFFF // 18-bit ADC values
		ch := Channel{
			Type:  AnalogIn,
			Range: rng,
			Cal:   &CalEntry{Slope: slope, Offset: offset},
		}

		v := calibrate(raw, ch)

		if math.IsNaN(v) {
			t.Fatalf("calibrate returned NaN for raw=%d range=%d slope=%f offset=%f",
				raw, rng, slope, offset)
		}
		if math.IsInf(v, 0) {
			t.Fatalf("calibrate returned Inf for raw=%d range=%d slope=%f offset=%f",
				raw, rng, slope, offset)
		}

		// Unipolar ranges clamp at 0.
		switch rng {
		case 2: // UP10V
			if v < -0.001 {
				t.Errorf("UP10V: got %f, expected >= 0", v)
			}
		case 3: // UP5V
			if v < -0.001 {
				t.Errorf("UP5V: got %f, expected >= 0", v)
			}
		}
	})
}

// FuzzCaptureSeekable emulates a seekable capture (writing to a real file)
// to exercise frame-count patching in Writer.Close, then reads back via
// NewReader and verifies FrameCount and data integrity.
func FuzzCaptureSeekable(f *testing.F) {
	for _, nCh := range []int{1, 4, 8, 13} {
		for _, nFrames := range []int{0, 1, 50, 500} {
			f.Add(nCh, nFrames)
		}
	}

	f.Fuzz(func(t *testing.T, nCh, nFrames int) {
		if nCh < 1 || nCh > 13 || nFrames < 0 || nFrames > 5000 {
			t.Skip()
		}

		tmpFile, err := os.CreateTemp("", "fuzz-capture-*.daq")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(tmpFile.Name())
		defer tmpFile.Close()

		var queue []int
		for i := range nCh {
			queue = append(queue, i%13)
		}

		h := buildFuzzHeader(queue, make([]uint8, nCh), RawUint32, 48000)

		w, err := NewWriter(tmpFile, h)
		if err != nil {
			t.Fatal(err)
		}

		for i := range nFrames {
			vals := make([]uint32, nCh)
			for j := range nCh {
				vals[j] = uint32(i*nCh+j) & 0x3FFFF
			}
			if err := w.WriteFrame(vals); err != nil {
				t.Fatalf("write %d: %v", i, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		// Re-open and verify frame count was patched.
		rf, err := os.Open(tmpFile.Name())
		if err != nil {
			t.Fatal(err)
		}
		defer rf.Close()

		r, err := NewReader(rf)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()

		if r.Header().FrameCount != uint64(nFrames) {
			t.Fatalf("FrameCount = %d, want %d", r.Header().FrameCount, nFrames)
		}

		count := 0
		for _, err := range r.Frames() {
			if err != nil {
				t.Fatalf("read frame %d: %v", count, err)
			}
			count++
		}
		if count != nFrames {
			t.Fatalf("read %d frames, want %d", count, nFrames)
		}
	})
}

// benchData generates numFrames of raw uint32 data for numCh channels.
func benchData(numCh, numFrames int) [][]uint32 {
	data := make([][]uint32, numFrames)
	for i := range numFrames {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			// Simulate slowly-varying ADC signal.
			vals[ch] = uint32(131072 + (i*13+ch*7)%1000)
		}
		data[i] = vals
	}
	return data
}


func BenchmarkWriteRaw(b *testing.B) {
	const numCh = 8
	data := benchData(numCh, 1)
	h := testHeaderRaw(numCh)
	frame := data[0]

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		w, _ := NewWriter(io.Discard, h)
		for range 10000 {
			w.WriteFrame(frame)
		}
		w.Close()
	}
}

func BenchmarkWriteRawCompressed(b *testing.B) {
	const numCh = 8
	data := benchData(numCh, 1)
	h := testHeaderRaw(numCh)
	frame := data[0]

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		w, _ := NewWriter(io.Discard, h, WithCompression(true))
		for range 10000 {
			w.WriteFrame(frame)
		}
		w.Close()
	}
}


func BenchmarkReadRaw(b *testing.B) {
	const numCh = 8
	const numFrames = 10000
	data := benchData(numCh, numFrames)
	h := testHeaderRaw(numCh)

	var buf bytes.Buffer
	w, _ := NewWriter(&buf, h)
	for _, f := range data {
		w.WriteFrame(f)
	}
	w.Close()
	raw := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		r, _ := NewReader(bytes.NewReader(raw))
		for {
			_, err := r.ReadFrame()
			if err != nil {
				break
			}
		}
		r.Close()
	}
}

func BenchmarkReadRawCompressed(b *testing.B) {
	const numCh = 8
	const numFrames = 10000
	data := benchData(numCh, numFrames)
	h := testHeaderRaw(numCh)

	var buf bytes.Buffer
	w, _ := NewWriter(&buf, h, WithCompression(true))
	for _, f := range data {
		w.WriteFrame(f)
	}
	w.Close()
	raw := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		r, _ := NewReader(bytes.NewReader(raw))
		for {
			_, err := r.ReadFrame()
			if err != nil {
				break
			}
		}
		r.Close()
	}
}


// BenchmarkWriteDiscard_Uncompressed vs Compressed isolates compression overhead
// by writing to io.Discard (no I/O cost).
func BenchmarkWriteDiscard_Uncompressed(b *testing.B) {
	const numCh = 8
	const numFrames = 50000
	data := benchData(numCh, numFrames)
	h := testHeaderRaw(numCh)

	b.SetBytes(int64(numFrames * numCh * 4))
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		w, _ := NewWriter(io.Discard, h)
		for _, f := range data {
			w.WriteFrame(f)
		}
		w.Close()
	}
}

func BenchmarkWriteDiscard_Compressed(b *testing.B) {
	const numCh = 8
	const numFrames = 50000
	data := benchData(numCh, numFrames)
	h := testHeaderRaw(numCh)

	b.SetBytes(int64(numFrames * numCh * 4))
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		w, _ := NewWriter(io.Discard, h, WithCompression(true))
		for _, f := range data {
			w.WriteFrame(f)
		}
		w.Close()
	}
}
