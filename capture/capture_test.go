package capture

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
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

// framesToBulk encodes uint32 frames as little-endian bytes suitable for WriteBulk.
func framesToBulk(frames [][]uint32) []byte {
	if len(frames) == 0 {
		return nil
	}
	numCh := len(frames[0])
	buf := make([]byte, len(frames)*numCh*4)
	for i, vals := range frames {
		for ch, v := range vals {
			binary.LittleEndian.PutUint32(buf[(i*numCh+ch)*4:], v)
		}
	}
	return buf
}

// writeRawFrames writes n frames of raw data via per-frame WriteBulk calls,
// returning the values written. Writing one frame at a time ensures segment
// rotation is triggered at the expected boundaries.
func writeRawFrames(t *testing.T, w *Writer, numCh, n int) [][]uint32 {
	t.Helper()
	frames := make([][]uint32, n)
	for i := range n {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			vals[ch] = uint32(i*numCh+ch) & 0x3FFFF // 18-bit range
		}
		frames[i] = vals
		if err := w.WriteBulk(framesToBulk([][]uint32{vals})); err != nil {
			t.Fatalf("WriteBulk frame %d: %v", i, err)
		}
	}
	return frames
}

// testCapture creates a capture directory with the given frames and returns the dir path.
func testCapture(t *testing.T, h Header, frames [][]uint32, opts ...WriterOption) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h, opts...)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames {
		if err := w.WriteBulk(framesToBulk([][]uint32{f})); err != nil {
			t.Fatalf("WriteBulk frame %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNewWriter_NoChannels(t *testing.T) {
	h := Header{Format: RawUint32}
	_, err := NewWriter(t.TempDir(), h)
	if !errors.Is(err, ErrNoChannels) {
		t.Fatalf("expected ErrNoChannels, got %v", err)
	}
}

func TestNewWriter_InvalidFormat(t *testing.T) {
	h := testHeaderRaw(2)
	h.Format = DataFormat(99)
	_, err := NewWriter(t.TempDir(), h)
	if !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("expected ErrInvalidFormat, got %v", err)
	}
}

func TestWriteBulk_AfterClose(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	if err := w.WriteBulk(framesToBulk([][]uint32{{1, 2}})); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("expected ErrWriterClosed, got %v", err)
	}
}

func TestFlush_AfterClose(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()

	if err := w.Flush(); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("expected ErrWriterClosed, got %v", err)
	}
}

func TestClose_DoubleClose(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, testHeaderRaw(2))
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

func TestNewReader_NotDirectory(t *testing.T) {
	f, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	_, err = NewReader(f.Name())
	if !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("expected ErrNotDirectory, got %v", err)
	}
}

func TestNewReader_EmptyDir(t *testing.T) {
	_, err := NewReader(t.TempDir())
	if !errors.Is(err, ErrNoSegments) {
		t.Fatalf("expected ErrNoSegments, got %v", err)
	}
}

func TestReadFrame_AfterClose(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{1, 2}})

	r, err := NewReader(dir)
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
	dir := testCapture(t, testHeaderRaw(2), nil)

	r, err := NewReader(dir)
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

// TestRoundTrip_RawUint32 writes and reads back RawUint32 frames.
func TestRoundTrip_RawUint32(t *testing.T) {
	const numCh = 4
	const numFrames = 100

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

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

	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after all frames, got %v", err)
	}
}

func TestRoundTrip_SingleFrame(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{0xAAAA, 0xBBBB}})

	r, err := NewReader(dir)
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

func TestRoundTrip_ZeroFrames(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), nil)

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF on empty capture, got %v", err)
	}
}

func TestRoundTrip_ManyChannels(t *testing.T) {
	const numCh = 13
	const numFrames = 10

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
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

// TestMultiSegment_Rotation verifies that frames are correctly split across segments.
func TestMultiSegment_Rotation(t *testing.T) {
	const numCh = 2
	const numFrames = 100
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	// Set segment size to hold ~30 frames.
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Should have multiple segment files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	segCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".daq" {
			segCount++
		}
	}
	if segCount < 3 {
		t.Errorf("expected at least 3 segments, got %d", segCount)
	}

	// No pending files should remain.
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".pending" {
			t.Errorf("pending file not cleaned up: %s", e.Name())
		}
	}

	// Read back all frames.
	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Header().FrameCount != numFrames {
		t.Errorf("aggregate FrameCount = %d, want %d", r.Header().FrameCount, numFrames)
	}

	count := 0
	for f, err := range r.Frames() {
		if err != nil {
			t.Fatalf("frame %d: %v", count, err)
		}
		for ch := range numCh {
			if f.RawValues()[ch] != written[count][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d",
					count, ch, f.RawValues()[ch], written[count][ch])
			}
		}
		count++
	}
	if count != numFrames {
		t.Errorf("read %d frames, want %d", count, numFrames)
	}
}

// TestMultiSegment_WriteBulk verifies WriteBulk across segment boundaries.
func TestMultiSegment_WriteBulk(t *testing.T) {
	const numCh = 2
	const numFrames = 100
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	if err != nil {
		t.Fatal(err)
	}

	// Build bulk data.
	written := make([][]uint32, numFrames)
	bulkData := make([]byte, numFrames*frameSize)
	for i := range numFrames {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			v := uint32(i*numCh+ch) & 0x3FFFF
			vals[ch] = v
			binary.LittleEndian.PutUint32(bulkData[i*frameSize+ch*4:], v)
		}
		written[i] = vals
	}

	if err := w.WriteBulk(bulkData); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if w.FramesWritten() != numFrames {
		t.Errorf("FramesWritten = %d, want %d", w.FramesWritten(), numFrames)
	}

	// Read back.
	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	count := 0
	for f, err := range r.Frames() {
		if err != nil {
			t.Fatalf("frame %d: %v", count, err)
		}
		for ch := range numCh {
			if f.RawValues()[ch] != written[count][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d",
					count, ch, f.RawValues()[ch], written[count][ch])
			}
		}
		count++
	}
	if count != numFrames {
		t.Errorf("read %d frames, want %d", count, numFrames)
	}
}

// TestMultiSegment_PendingCleanup verifies that unused pending files are removed on Close.
func TestMultiSegment_PendingCleanup(t *testing.T) {
	h := testHeaderRaw(2)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	// Write just one frame — no rotation needed.
	w.WriteBulk(framesToBulk([][]uint32{{1, 2}}))
	w.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".pending" {
			t.Errorf("pending file not cleaned up: %s", e.Name())
		}
	}
}

// TestMultiSegment_PreambleV2 verifies v2 preamble fields in segment files.
func TestMultiSegment_PreambleV2(t *testing.T) {
	const numCh = 2
	const numFrames = 100
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	if err != nil {
		t.Fatal(err)
	}
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var totalFrames uint64
	prevSeq := -1
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".daq" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		pre, err := readPreambleFromFile(path)
		if err != nil {
			t.Fatalf("read preamble %s: %v", e.Name(), err)
		}

		// Sequence numbers should be monotonically increasing.
		if int(pre.sequenceNumber) <= prevSeq {
			t.Errorf("segment %s: seq %d not after %d", e.Name(), pre.sequenceNumber, prevSeq)
		}
		prevSeq = int(pre.sequenceNumber)

		// Global frame offset should match running total.
		if pre.globalFrameOffset != totalFrames {
			t.Errorf("segment %s: globalFrameOffset = %d, want %d",
				e.Name(), pre.globalFrameOffset, totalFrames)
		}

		totalFrames += pre.frameCount
	}

	if totalFrames != numFrames {
		t.Errorf("total frames across segments = %d, want %d", totalFrames, numFrames)
	}
}

// TestRandomAccess_ReadFrames verifies random access across segment boundaries.
func TestRandomAccess_ReadFrames(t *testing.T) {
	const numCh = 2
	const numFrames = 100
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Read from the middle.
	frames, err := r.ReadFrames(40, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 20 {
		t.Fatalf("got %d frames, want 20", len(frames))
	}
	for i, f := range frames {
		raw := f.RawValues()
		for ch := range numCh {
			if raw[ch] != written[40+i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", 40+i, ch, raw[ch], written[40+i][ch])
			}
		}
	}

	// Read past the end.
	frames, err = r.ReadFrames(90, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 10 {
		t.Fatalf("got %d frames from offset 90, want 10", len(frames))
	}

	// Read from beyond the end.
	frames, err = r.ReadFrames(200, 10)
	if err != nil {
		t.Fatal(err)
	}
	if frames != nil {
		t.Errorf("expected nil for out-of-range offset, got %d frames", len(frames))
	}
}

// TestRandomAccess_CrossSegmentBoundary reads frames that span two segments.
func TestRandomAccess_CrossSegmentBoundary(t *testing.T) {
	const numCh = 2
	const numFrames = 100
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Read frames that span a segment boundary (around frame 30).
	frames, err := r.ReadFrames(25, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 15 {
		t.Fatalf("got %d frames, want 15", len(frames))
	}
	for i, f := range frames {
		raw := f.RawValues()
		for ch := range numCh {
			if raw[ch] != written[25+i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", 25+i, ch, raw[ch], written[25+i][ch])
			}
		}
	}
}

// TestSegmentFilter verifies that NewReader with segment numbers filters correctly.
func TestSegmentFilter(t *testing.T) {
	const numCh = 2
	const numFrames = 100
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	if err != nil {
		t.Fatal(err)
	}
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	// Read only segment 1.
	r, err := NewReader(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	count := 0
	for _, err := range r.Frames() {
		if err != nil {
			t.Fatalf("frame %d: %v", count, err)
		}
		count++
	}
	if count == 0 {
		t.Error("no frames from segment 1")
	}
	if count >= numFrames {
		t.Errorf("expected fewer frames from segment 1, got %d", count)
	}
}

// TestSegmentFilter_InvalidSegment verifies error when requesting non-existent segment.
func TestSegmentFilter_InvalidSegment(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{1, 2}})

	_, err := NewReader(dir, 999)
	if !errors.Is(err, ErrNoSegments) {
		t.Fatalf("expected ErrNoSegments, got %v", err)
	}
}

// Calibration tests — these don't depend on Writer/Reader API changes.

func TestCalibrate_BP10V(t *testing.T) {
	ch := Channel{Type: AnalogIn, Range: 0, Cal: &CalEntry{Slope: 1.0, Offset: 0.0}}

	got := calibrate(131072, ch)
	if got != 0.0 {
		t.Errorf("midpoint: got %f, want 0.0", got)
	}

	got = calibrate(0, ch)
	if got != -10.0 {
		t.Errorf("zero: got %f, want -10.0", got)
	}

	got = calibrate(0x3FFFF, ch)
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
	ch := Channel{Type: AnalogIn, Range: 2, Cal: &CalEntry{Slope: 1.0, Offset: -1000.0}}
	got := calibrate(100, ch)
	if got != 0.0 {
		t.Errorf("unipolar clamp low: got %f, want 0.0", got)
	}
}

func TestFrame_Values_RawWithCalibration(t *testing.T) {
	h := testHeaderMixed()
	frame := []uint32{131072, 131072, 0xFF, 1000, 500}
	dir := testCapture(t, h, [][]uint32{frame})

	r, err := NewReader(dir)
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
	if vals[0] != 0.0 {
		t.Errorf("ch0 (BP10V midpoint): got %f, want 0.0", vals[0])
	}
	if vals[1] != 0.0 {
		t.Errorf("ch1 (BP5V midpoint): got %f, want 0.0", vals[1])
	}
	if vals[2] != 255.0 {
		t.Errorf("ch2 (digital): got %f, want 255.0", vals[2])
	}
	if vals[3] != 1000.0 {
		t.Errorf("ch3 (counter): got %f, want 1000.0", vals[3])
	}
	if vals[4] != 500.0 {
		t.Errorf("ch4 (encoder): got %f, want 500.0", vals[4])
	}
}

func TestFrames_Iterator(t *testing.T) {
	const numCh = 2
	const numFrames = 10

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
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

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
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

func TestWithBufferSize(t *testing.T) {
	const numCh = 2
	const numFrames = 10

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h, WithBufferSize(2))
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
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
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{
		{1, 2}, {3, 4}, {5, 6},
	}, WithBufferSize(100))

	r, err := NewReader(dir)
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

func TestFramesWritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, testHeaderRaw(2))
	if err != nil {
		t.Fatal(err)
	}

	if w.FramesWritten() != 0 {
		t.Errorf("before writes: got %d, want 0", w.FramesWritten())
	}

	w.WriteBulk(framesToBulk([][]uint32{{1, 2}, {3, 4}, {5, 6}}))

	if w.FramesWritten() != 3 {
		t.Errorf("after 3 writes: got %d, want 3", w.FramesWritten())
	}

	w.Close()

	if w.FramesWritten() != 3 {
		t.Errorf("after close: got %d, want 3", w.FramesWritten())
	}
}

func TestFrameCount_Patched(t *testing.T) {
	h := testHeaderRaw(2)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	w.WriteBulk(framesToBulk([][]uint32{{1, 2}, {3, 4}, {5, 6}}))
	w.Close()

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Header().FrameCount != 3 {
		t.Errorf("FrameCount = %d, want 3", r.Header().FrameCount)
	}
}

func TestFrame_Reuse(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{
		{100, 200}, {300, 400},
	})

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	f1, _ := r.ReadFrame()
	raw1Ptr := &f1.RawValues()[0]

	f2, _ := r.ReadFrame()
	raw2Ptr := &f2.RawValues()[0]

	if raw1Ptr != raw2Ptr {
		t.Error("expected ReadFrame to reuse internal slice")
	}
	if f2.RawValues()[0] != 300 || f2.RawValues()[1] != 400 {
		t.Errorf("second frame: got [%d, %d], want [300, 400]",
			f2.RawValues()[0], f2.RawValues()[1])
	}
}

func TestHeader_RoundTrip(t *testing.T) {
	h := testHeaderMixed()
	dir := testCapture(t, h, nil)

	r, err := NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	rh := r.Header()

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
	if rh.SampleRate != h.SampleRate {
		t.Errorf("SampleRate = %f, want %f", rh.SampleRate, h.SampleRate)
	}
	if rh.Format != h.Format {
		t.Errorf("Format = %d, want %d", rh.Format, h.Format)
	}
	if rh.Timestamp != h.Timestamp {
		t.Errorf("Timestamp = %d, want %d", rh.Timestamp, h.Timestamp)
	}
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

	if len(rh.Properties) != len(h.Properties) {
		t.Fatalf("Properties len = %d, want %d", len(rh.Properties), len(h.Properties))
	}
	for k, v := range h.Properties {
		if rh.Properties[k] != v {
			t.Errorf("Properties[%q] = %q, want %q", k, rh.Properties[k], v)
		}
	}

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

// channelTypes maps hardware indices to channel types.
var channelTypes = [13]ChannelType{
	AnalogIn, AnalogIn, AnalogIn, AnalogIn,
	AnalogIn, AnalogIn, AnalogIn, AnalogIn,
	DigitalIO,
	Counter, Counter,
	Encoder, Encoder,
}

func buildFuzzHeader(queue []int, ranges []uint8, format DataFormat, rate float64) Header {
	channels := make([]Channel, len(queue))
	analogIdx := 0
	for i, idx := range queue {
		ch := Channel{
			Index: idx,
			Type:  channelTypes[idx],
		}
		if idx < 8 {
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

func FuzzCaptureRaw(f *testing.F) {
	for _, queueBits := range []uint16{
		0x000F, 0x00FF, 0x0003, 0x0103, 0x1F03, 0x0001, 0x1FFF,
	} {
		for _, rangeBits := range []uint8{0, 1, 2, 3} {
			for _, nFrames := range []int{0, 1, 10, 100} {
				for _, bufSize := range []int{1, 64, 1024} {
					f.Add(queueBits, rangeBits, nFrames, bufSize)
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, queueBits uint16, rangeBits uint8, nFrames int, bufSize int) {
		if nFrames < 0 || nFrames > 5000 || bufSize < 1 || bufSize > 100000 {
			t.Skip()
		}
		queueBits &= 0x1FFF
		if queueBits == 0 {
			t.Skip()
		}

		var queue []int
		for i := range 13 {
			if queueBits&(1<<i) != 0 {
				queue = append(queue, i)
			}
		}

		var ranges []uint8
		for _, idx := range queue {
			if idx < 8 {
				ranges = append(ranges, rangeBits%4)
			}
		}

		h := buildFuzzHeader(queue, ranges, RawUint32, 10000)
		nCh := len(queue)

		dir := filepath.Join(t.TempDir(), "capture")
		opts := []WriterOption{WithBufferSize(bufSize)}

		w, err := NewWriter(dir, h, opts...)
		if err != nil {
			t.Fatal(err)
		}

		allFrames := make([][]uint32, nFrames)
		for i := range nFrames {
			vals := make([]uint32, nCh)
			for j, idx := range queue {
				switch {
				case idx < 8:
					vals[j] = uint32(131072+i*13+j*7) & 0x3FFFF
				case idx == 8:
					vals[j] = uint32(i) & 0xFF
				default:
					vals[j] = uint32(i * (idx + 1))
				}
			}
			allFrames[i] = vals
			if err := w.WriteBulk(framesToBulk([][]uint32{vals})); err != nil {
				t.Fatalf("WriteBulk %d: %v", i, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		if w.FramesWritten() != uint64(nFrames) {
			t.Fatalf("FramesWritten = %d, want %d", w.FramesWritten(), nFrames)
		}

		r, err := NewReader(dir)
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

			raw := frame.RawValues()
			if len(raw) != nCh {
				t.Fatalf("frame %d: %d values, want %d", count, len(raw), nCh)
			}
			for j := range nCh {
				if raw[j] != allFrames[count][j] {
					t.Fatalf("frame %d ch %d: got %d, want %d", count, j, raw[j], allFrames[count][j])
				}
			}

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

		raw &= 0x3FFFF
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

		switch rng {
		case 2:
			if v < -0.001 {
				t.Errorf("UP10V: got %f, expected >= 0", v)
			}
		case 3:
			if v < -0.001 {
				t.Errorf("UP5V: got %f, expected >= 0", v)
			}
		}
	})
}

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

		var queue []int
		for i := range nCh {
			queue = append(queue, i%13)
		}

		h := buildFuzzHeader(queue, make([]uint8, nCh), RawUint32, 48000)

		dir := filepath.Join(t.TempDir(), "capture")
		w, err := NewWriter(dir, h)
		if err != nil {
			t.Fatal(err)
		}

		for i := range nFrames {
			vals := make([]uint32, nCh)
			for j := range nCh {
				vals[j] = uint32(i*nCh+j) & 0x3FFFF
			}
			if err := w.WriteBulk(framesToBulk([][]uint32{vals})); err != nil {
				t.Fatalf("WriteBulk %d: %v", i, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		r, err := NewReader(dir)
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

func benchData(numCh, numFrames int) [][]uint32 {
	data := make([][]uint32, numFrames)
	for i := range numFrames {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			vals[ch] = uint32(131072 + (i*13+ch*7)%1000)
		}
		data[i] = vals
	}
	return data
}

func BenchmarkWriteRaw(b *testing.B) {
	const numCh = 8
	const numFrames = 10000
	data := benchData(numCh, numFrames)
	h := testHeaderRaw(numCh)
	bulk := framesToBulk(data)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		dir := b.TempDir()
		w, _ := NewWriter(dir, h)
		w.WriteBulk(bulk)
		w.Close()
	}
}

func BenchmarkReadRaw(b *testing.B) {
	const numCh = 8
	const numFrames = 10000
	data := benchData(numCh, numFrames)
	h := testHeaderRaw(numCh)

	dir := b.TempDir()
	w, _ := NewWriter(dir, h)
	w.WriteBulk(framesToBulk(data))
	w.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		r, _ := NewReader(dir)
		for {
			_, err := r.ReadFrame()
			if err != nil {
				break
			}
		}
		r.Close()
	}
}
