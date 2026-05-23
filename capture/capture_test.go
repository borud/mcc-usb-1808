package capture

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/borud/mcc-usb-1808/v4/device"
	"github.com/stretchr/testify/require"
)

func testHeaderRaw(channels int) Header {
	chs := make([]Channel, channels)
	for i := range chs {
		chs[i] = Channel{
			Index: i,
			Type:  device.ChannelTypeAnalog,
			Range: 0, // BP10V
			Cal:   &device.Calibration{Slope: 1.0, Offset: 0.0},
		}
	}
	return Header{
		DeviceModel:     "USB-1808",
		DeviceSerial:    "12345678",
		FPGAVersion:     "1.5",
		CalibrationDate: time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC),
		Channels:        chs,
		SampleRate:      1000,
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
			{Index: 0, Type: device.ChannelTypeAnalog, Range: 0, Cal: &device.Calibration{Slope: 1.0, Offset: 0.0}},
			{Index: 1, Type: device.ChannelTypeAnalog, Range: 1, Cal: &device.Calibration{Slope: 1.0, Offset: 0.0}},
			{Index: 8, Type: device.ChannelTypeDIO},
			{Index: 9, Type: device.ChannelTypeCounter},
			{Index: 11, Type: device.ChannelTypeEncoder},
		},
		SampleRate:      10000,
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
		require.NoErrorf(t, w.WriteBulk(framesToBulk([][]uint32{vals})), "WriteBulk frame %d", i)
	}
	return frames
}

// testCapture creates a capture directory with the given frames and returns the dir path.
func testCapture(t *testing.T, h Header, frames [][]uint32, opts ...WriterOption) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h, opts...)
	require.NoError(t, err)
	for i, f := range frames {
		require.NoErrorf(t, w.WriteBulk(framesToBulk([][]uint32{f})), "WriteBulk frame %d", i)
	}
	require.NoError(t, w.Close())
	return dir
}

func TestNewWriter_NoChannels(t *testing.T) {
	h := Header{}
	_, err := NewWriter(filepath.Join(t.TempDir(), "capture"), h)
	require.ErrorIs(t, err, ErrNoChannels)
}

func TestWriteBulk_AfterClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, testHeaderRaw(2))
	require.NoError(t, err)
	w.Close()

	require.ErrorIs(t, w.WriteBulk(framesToBulk([][]uint32{{1, 2}})), ErrWriterClosed)
}

func TestFlush_AfterClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, testHeaderRaw(2))
	require.NoError(t, err)
	w.Close()

	require.ErrorIs(t, w.Flush(), ErrWriterClosed)
}

func TestClose_DoubleClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, testHeaderRaw(2))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.ErrorIs(t, w.Close(), ErrWriterClosed)
}

func TestNewReader_NotDirectory(t *testing.T) {
	f, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	_, err = NewReader(f.Name())
	require.ErrorIs(t, err, ErrNotDirectory)
}

func TestNewReader_EmptyDir(t *testing.T) {
	_, err := NewReader(t.TempDir())
	require.ErrorIs(t, err, ErrNoSegments)
}

func TestReadFrame_AfterClose(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{1, 2}})

	r, err := NewReader(dir)
	require.NoError(t, err)
	r.Close()

	_, err = r.ReadFrame()
	require.ErrorIs(t, err, ErrReaderClosed)
}

func TestReaderClose_DoubleClose(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), nil)

	r, err := NewReader(dir)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.ErrorIs(t, r.Close(), ErrReaderClosed)
}

// TestRoundTrip_RawUint32 writes and reads back RawUint32 frames.
func TestRoundTrip_RawUint32(t *testing.T) {
	const numCh = 4
	const numFrames = 100

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	require.NoError(t, w.Close())

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	rh := r.Header()
	require.Equal(t, h.DeviceModel, rh.DeviceModel)
	require.Len(t, rh.Channels, numCh)

	for i := range numFrames {
		f, err := r.ReadFrame()
		require.NoErrorf(t, err, "ReadFrame %d", i)
		raw := f.RawValues()
		require.Lenf(t, raw, numCh, "frame %d", i)
		for ch := range numCh {
			require.Equalf(t, written[i][ch], raw[ch], "frame %d ch %d", i, ch)
		}
	}

	_, err = r.ReadFrame()
	require.ErrorIs(t, err, io.EOF)
}

func TestRoundTrip_SingleFrame(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{0xAAAA, 0xBBBB}})

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	f, err := r.ReadFrame()
	require.NoError(t, err)
	raw := f.RawValues()
	require.Equal(t, uint32(0xAAAA), raw[0])
	require.Equal(t, uint32(0xBBBB), raw[1])

	_, err = r.ReadFrame()
	require.ErrorIs(t, err, io.EOF)
}

func TestRoundTrip_ZeroFrames(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), nil)

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	_, err = r.ReadFrame()
	require.ErrorIs(t, err, io.EOF)
}

func TestRoundTrip_ManyChannels(t *testing.T) {
	const numCh = 13
	const numFrames = 10

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	for i := range numFrames {
		f, err := r.ReadFrame()
		require.NoErrorf(t, err, "ReadFrame %d", i)
		for ch := range numCh {
			require.Equalf(t, written[i][ch], f.RawValues()[ch], "frame %d ch %d", i, ch)
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
	segSize := frameSize * 30
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	require.NoError(t, w.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	segCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".daq" {
			segCount++
		}
	}
	require.GreaterOrEqual(t, segCount, 3)

	for _, e := range entries {
		require.NotEqual(t, ".pending", filepath.Ext(e.Name()), "pending file not cleaned up: %s", e.Name())
	}

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	require.Equal(t, uint64(numFrames), r.Header().FrameCount)

	count := 0
	for f, err := range r.Frames() {
		require.NoErrorf(t, err, "frame %d", count)
		for ch := range numCh {
			require.Equalf(t, written[count][ch], f.RawValues()[ch], "frame %d ch %d", count, ch)
		}
		count++
	}
	require.Equal(t, numFrames, count)
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
	require.NoError(t, err)

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

	require.NoError(t, w.WriteBulk(bulkData))
	require.NoError(t, w.Close())
	require.Equal(t, uint64(numFrames), w.FramesWritten())

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	count := 0
	for f, err := range r.Frames() {
		require.NoErrorf(t, err, "frame %d", count)
		for ch := range numCh {
			require.Equalf(t, written[count][ch], f.RawValues()[ch], "frame %d ch %d", count, ch)
		}
		count++
	}
	require.Equal(t, numFrames, count)
}

// TestMultiSegment_PendingCleanup verifies that unused pending files are removed on Close.
func TestMultiSegment_PendingCleanup(t *testing.T) {
	h := testHeaderRaw(2)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	w.WriteBulk(framesToBulk([][]uint32{{1, 2}}))
	w.Close()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotEqual(t, ".pending", filepath.Ext(e.Name()), "pending file not cleaned up: %s", e.Name())
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
	require.NoError(t, err)
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var totalFrames uint64
	prevSeq := -1
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".daq" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		pre, err := readPreambleFromFile(path)
		require.NoErrorf(t, err, "read preamble %s", e.Name())

		require.Greaterf(t, int(pre.sequenceNumber), prevSeq, "segment %s: seq not increasing", e.Name())
		prevSeq = int(pre.sequenceNumber)

		require.Equalf(t, totalFrames, pre.globalFrameOffset,
			"segment %s: globalFrameOffset", e.Name())

		totalFrames += pre.frameCount
	}

	require.Equal(t, uint64(numFrames), totalFrames)
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
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	// Read from the middle.
	frames, err := r.ReadFrames(40, 20)
	require.NoError(t, err)
	require.Len(t, frames, 20)
	for i, f := range frames {
		raw := f.RawValues()
		for ch := range numCh {
			require.Equalf(t, written[40+i][ch], raw[ch], "frame %d ch %d", 40+i, ch)
		}
	}

	// Read past the end.
	frames, err = r.ReadFrames(90, 20)
	require.NoError(t, err)
	require.Len(t, frames, 10)

	// Read from beyond the end.
	frames, err = r.ReadFrames(200, 10)
	require.NoError(t, err)
	require.Nil(t, frames)
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
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	frames, err := r.ReadFrames(25, 15)
	require.NoError(t, err)
	require.Len(t, frames, 15)
	for i, f := range frames {
		raw := f.RawValues()
		for ch := range numCh {
			require.Equalf(t, written[25+i][ch], raw[ch], "frame %d ch %d", 25+i, ch)
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
	require.NoError(t, err)
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir, 1)
	require.NoError(t, err)
	defer r.Close()

	count := 0
	for _, err := range r.Frames() {
		require.NoErrorf(t, err, "frame %d", count)
		count++
	}
	require.Greater(t, count, 0, "no frames from segment 1")
	require.Less(t, count, numFrames, "expected fewer frames from segment 1")
}

// TestSegmentFilter_InvalidSegment verifies error when requesting non-existent segment.
func TestSegmentFilter_InvalidSegment(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{1, 2}})

	_, err := NewReader(dir, 999)
	require.ErrorIs(t, err, ErrNoSegments)
}

// Calibration tests — these don't depend on Writer/Reader API changes.

func TestCalibrate_BP10V(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 0, Cal: &device.Calibration{Slope: 1.0, Offset: 0.0}}

	require.Equal(t, 0.0, calibrate(device.ADCMidscale, ch))
	require.Equal(t, -10.0, calibrate(0, ch))

	got := calibrate(device.ADCFullScale, ch)
	want := (float64(device.ADCFullScale) - device.ADCMidscale) * 10.0 / device.ADCMidscale
	require.InDelta(t, want, got, 1e-6)
}

func TestCalibrate_BP5V(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 1, Cal: &device.Calibration{Slope: 1.0, Offset: 0.0}}

	require.Equal(t, 0.0, calibrate(device.ADCMidscale, ch))
	require.Equal(t, -5.0, calibrate(0, ch))
}

func TestCalibrate_UP10V(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 2, Cal: &device.Calibration{Slope: 1.0, Offset: 0.0}}

	require.Equal(t, 0.0, calibrate(0, ch))
	require.InDelta(t, 10.0, calibrate(0x3FFFF, ch), 1e-6)
}

func TestCalibrate_UP5V(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 3, Cal: &device.Calibration{Slope: 1.0, Offset: 0.0}}

	require.InDelta(t, 5.0, calibrate(0x3FFFF, ch), 1e-6)
}

func TestCalibrate_WithSlopeOffset(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 0, Cal: &device.Calibration{Slope: 1.0005, Offset: 10.0}}

	raw := uint32(device.ADCMidscale)
	cal := float64(raw)*float64(ch.Cal.Slope) + float64(ch.Cal.Offset)
	cal = math.Round(cal)
	want := (cal - device.ADCMidscale) * 10.0 / device.ADCMidscale

	require.InDelta(t, want, calibrate(raw, ch), 1e-9)
}

func TestCalibrate_NonAnalogPassthrough(t *testing.T) {
	for _, ct := range []device.ChannelType{device.ChannelTypeDIO, device.ChannelTypeCounter, device.ChannelTypeEncoder} {
		ch := Channel{Type: ct}
		require.Equal(t, 42.0, calibrate(42, ch))
	}
}

func TestCalibrate_NilCal(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 0, Cal: nil}
	require.Equal(t, 1000.0, calibrate(1000, ch))
}

func TestCalibrate_UnipolarClamp(t *testing.T) {
	ch := Channel{Type: device.ChannelTypeAnalog, Range: 2, Cal: &device.Calibration{Slope: 1.0, Offset: -1000.0}}
	require.Equal(t, 0.0, calibrate(100, ch))
}

func TestFrame_Values_RawWithCalibration(t *testing.T) {
	h := testHeaderMixed()
	frame := []uint32{device.ADCMidscale, device.ADCMidscale, 0xFF, 1000, 500}
	dir := testCapture(t, h, [][]uint32{frame})

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	f, err := r.ReadFrame()
	require.NoError(t, err)

	vals := f.Values()
	require.Len(t, vals, 5)
	require.Equal(t, 0.0, vals[0], "ch0 (BP10V midpoint)")
	require.Equal(t, 0.0, vals[1], "ch1 (BP5V midpoint)")
	require.Equal(t, 255.0, vals[2], "ch2 (digital)")
	require.Equal(t, 1000.0, vals[3], "ch3 (counter)")
	require.Equal(t, 500.0, vals[4], "ch4 (encoder)")
}

func TestFrames_Iterator(t *testing.T) {
	const numCh = 2
	const numFrames = 10

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	count := 0
	for f, err := range r.Frames() {
		require.NoErrorf(t, err, "frame %d", count)
		raw := f.RawValues()
		for ch := range numCh {
			require.Equalf(t, written[count][ch], raw[ch], "frame %d ch %d", count, ch)
		}
		count++
	}
	require.Equal(t, numFrames, count)
}

func TestFrames_Iterator_EarlyBreak(t *testing.T) {
	const numCh = 2
	const numFrames = 100

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	count := 0
	for _, err := range r.Frames() {
		require.NoError(t, err)
		count++
		if count == 5 {
			break
		}
	}
	require.Equal(t, 5, count)
}

func TestWithBufferSize(t *testing.T) {
	const numCh = 2
	const numFrames = 10

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h, WithBufferSize(2))
	require.NoError(t, err)
	written := writeRawFrames(t, w, numCh, numFrames)
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	for i := range numFrames {
		f, err := r.ReadFrame()
		require.NoErrorf(t, err, "ReadFrame %d", i)
		for ch := range numCh {
			require.Equalf(t, written[i][ch], f.RawValues()[ch], "frame %d ch %d", i, ch)
		}
	}
}

func TestClose_FlushesPartialBuffer(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{
		{1, 2}, {3, 4}, {5, 6},
	}, WithBufferSize(100))

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	for i := range 3 {
		_, err := r.ReadFrame()
		require.NoErrorf(t, err, "ReadFrame %d", i)
	}
	_, err = r.ReadFrame()
	require.ErrorIs(t, err, io.EOF)
}

func TestFramesWritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, testHeaderRaw(2))
	require.NoError(t, err)

	require.Equal(t, uint64(0), w.FramesWritten())
	w.WriteBulk(framesToBulk([][]uint32{{1, 2}, {3, 4}, {5, 6}}))
	require.Equal(t, uint64(3), w.FramesWritten())
	w.Close()
	require.Equal(t, uint64(3), w.FramesWritten())
}

func TestFrameCount_Patched(t *testing.T) {
	h := testHeaderRaw(2)
	dir := filepath.Join(t.TempDir(), "capture")
	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	w.WriteBulk(framesToBulk([][]uint32{{1, 2}, {3, 4}, {5, 6}}))
	w.Close()

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	require.Equal(t, uint64(3), r.Header().FrameCount)
}

func TestFrame_Reuse(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{
		{100, 200}, {300, 400},
	})

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	f1, _ := r.ReadFrame()
	raw1Ptr := &f1.RawValues()[0]

	f2, _ := r.ReadFrame()
	raw2Ptr := &f2.RawValues()[0]

	require.Same(t, raw1Ptr, raw2Ptr, "expected ReadFrame to reuse internal slice")
	require.Equal(t, uint32(300), f2.RawValues()[0])
	require.Equal(t, uint32(400), f2.RawValues()[1])
}

func TestHeader_RoundTrip(t *testing.T) {
	h := testHeaderMixed()
	dir := testCapture(t, h, nil)

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	rh := r.Header()

	require.Equal(t, h.DeviceModel, rh.DeviceModel)
	require.Equal(t, h.DeviceSerial, rh.DeviceSerial)
	require.Equal(t, h.FPGAVersion, rh.FPGAVersion)
	require.True(t, rh.CalibrationDate.Equal(h.CalibrationDate))
	require.Equal(t, h.SampleRate, rh.SampleRate)
	require.Equal(t, h.Timestamp, rh.Timestamp)
	require.Equal(t, h.ApplicationName, rh.ApplicationName)
	require.Equal(t, h.SessionID, rh.SessionID)
	require.Equal(t, h.Description, rh.Description)
	require.Equal(t, h.Operator, rh.Operator)

	require.Len(t, rh.Properties, len(h.Properties))
	for k, v := range h.Properties {
		require.Equalf(t, v, rh.Properties[k], "Properties[%q]", k)
	}

	require.Len(t, rh.Channels, len(h.Channels))
	for i, ch := range rh.Channels {
		want := h.Channels[i]
		require.Equalf(t, want.Index, ch.Index, "channel %d Index", i)
		require.Equalf(t, want.Type, ch.Type, "channel %d Type", i)
		require.Equalf(t, want.Range, ch.Range, "channel %d Range", i)
		if want.Cal != nil {
			require.NotNilf(t, ch.Cal, "channel %d cal", i)
			require.Equalf(t, want.Cal.Slope, ch.Cal.Slope, "channel %d cal.Slope", i)
			require.Equalf(t, want.Cal.Offset, ch.Cal.Offset, "channel %d cal.Offset", i)
		}
	}
}

// channelTypes maps hardware indices to channel types.
var channelTypes = [13]device.ChannelType{
	device.ChannelTypeAnalog, device.ChannelTypeAnalog, device.ChannelTypeAnalog, device.ChannelTypeAnalog,
	device.ChannelTypeAnalog, device.ChannelTypeAnalog, device.ChannelTypeAnalog, device.ChannelTypeAnalog,
	device.ChannelTypeDIO,
	device.ChannelTypeCounter, device.ChannelTypeCounter,
	device.ChannelTypeEncoder, device.ChannelTypeEncoder,
}

func buildFuzzHeader(queue []int, ranges []uint8, rate float64) Header {
	channels := make([]Channel, len(queue))
	analogIdx := 0
	for i, idx := range queue {
		ch := Channel{
			Index: idx,
			Type:  channelTypes[idx],
		}
		if idx < 8 {
			ch.Range = device.Range(ranges[analogIdx])
			ch.Cal = &device.Calibration{
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

		h := buildFuzzHeader(queue, ranges, 10000)
		nCh := len(queue)

		dir := filepath.Join(t.TempDir(), "capture")
		opts := []WriterOption{WithBufferSize(bufSize)}

		w, err := NewWriter(dir, h, opts...)
		require.NoError(t, err)

		allFrames := make([][]uint32, nFrames)
		for i := range nFrames {
			vals := make([]uint32, nCh)
			for j, idx := range queue {
				switch {
				case idx < 8:
					vals[j] = uint32(device.ADCMidscale+i*13+j*7) & 0x3FFFF
				case idx == 8:
					vals[j] = uint32(i) & 0xFF
				default:
					vals[j] = uint32(i * (idx + 1))
				}
			}
			allFrames[i] = vals
			require.NoErrorf(t, w.WriteBulk(framesToBulk([][]uint32{vals})), "WriteBulk %d", i)
		}
		require.NoError(t, w.Close())
		require.Equal(t, uint64(nFrames), w.FramesWritten())

		r, err := NewReader(dir)
		require.NoError(t, err)
		defer r.Close()

		rh := r.Header()
		require.Len(t, rh.Channels, nCh)
		require.Equal(t, float64(10000), rh.SampleRate)

		count := 0
		for frame, err := range r.Frames() {
			require.NoErrorf(t, err, "Frames() at %d", count)

			raw := frame.RawValues()
			require.Lenf(t, raw, nCh, "frame %d", count)
			for j := range nCh {
				require.Equalf(t, allFrames[count][j], raw[j], "frame %d ch %d", count, j)
			}

			vals := frame.Values()
			for j, v := range vals {
				require.Falsef(t, math.IsNaN(v) || math.IsInf(v, 0),
					"frame %d ch %d: Values() = %f", count, j, v)
			}
			count++
		}
		require.Equal(t, nFrames, count)
	})
}

func FuzzCalibrate(f *testing.F) {
	for _, rng := range []uint8{0, 1, 2, 3} {
		for _, raw := range []uint32{0, 1, device.ADCMidscale, device.ADCFullScale} {
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
			Type:  device.ChannelTypeAnalog,
			Range: device.Range(rng),
			Cal:   &device.Calibration{Slope: slope, Offset: offset},
		}

		v := calibrate(raw, ch)

		require.Falsef(t, math.IsNaN(v), "calibrate returned NaN for raw=%d range=%d slope=%f offset=%f",
			raw, rng, slope, offset)
		require.Falsef(t, math.IsInf(v, 0), "calibrate returned Inf for raw=%d range=%d slope=%f offset=%f",
			raw, rng, slope, offset)

		switch rng {
		case 2:
			require.GreaterOrEqualf(t, v, -0.001, "UP10V")
		case 3:
			require.GreaterOrEqualf(t, v, -0.001, "UP5V")
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

		h := buildFuzzHeader(queue, make([]uint8, nCh), 48000)

		dir := filepath.Join(t.TempDir(), "capture")
		w, err := NewWriter(dir, h)
		require.NoError(t, err)

		for i := range nFrames {
			vals := make([]uint32, nCh)
			for j := range nCh {
				vals[j] = uint32(i*nCh+j) & 0x3FFFF
			}
			require.NoErrorf(t, w.WriteBulk(framesToBulk([][]uint32{vals})), "WriteBulk %d", i)
		}
		require.NoError(t, w.Close())

		r, err := NewReader(dir)
		require.NoError(t, err)
		defer r.Close()

		require.Equal(t, uint64(nFrames), r.Header().FrameCount)

		count := 0
		for _, err := range r.Frames() {
			require.NoErrorf(t, err, "read frame %d", count)
			count++
		}
		require.Equal(t, nFrames, count)
	})
}

// TestOverrunTrailer_RoundTrip verifies that overrun events survive write+read.
func TestOverrunTrailer_RoundTrip(t *testing.T) {
	const numCh = 2
	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")

	w, err := NewWriter(dir, h)
	require.NoError(t, err)

	w.WriteBulk(framesToBulk([][]uint32{{1, 2}, {3, 4}, {5, 6}}))
	w.RecordOverrun(OverrunEvent{FrameIndex: 2, Timestamp: 1700000000000})
	w.WriteBulk(framesToBulk([][]uint32{{7, 8}, {9, 10}}))
	w.RecordOverrun(OverrunEvent{FrameIndex: 4, Timestamp: 1700000001000, EstimatedLostFrames: 100})

	require.Equal(t, 2, w.OverrunCount())
	require.NoError(t, w.Close())

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	rh := r.Header()
	require.Equal(t, uint64(5), rh.FrameCount)
	require.Len(t, rh.Overruns, 2)
	require.Equal(t, uint64(2), rh.Overruns[0].FrameIndex)
	require.Equal(t, uint64(4), rh.Overruns[1].FrameIndex)
	require.Equal(t, uint64(100), rh.Overruns[1].EstimatedLostFrames)

	// Verify all frames are still readable.
	count := 0
	for _, err := range r.Frames() {
		require.NoError(t, err)
		count++
	}
	require.Equal(t, 5, count)
}

// TestOverrunTrailer_NoOverruns verifies backward compatibility (no trailer).
func TestOverrunTrailer_NoOverruns(t *testing.T) {
	dir := testCapture(t, testHeaderRaw(2), [][]uint32{{1, 2}, {3, 4}})

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	require.Empty(t, r.Header().Overruns)
}

// TestOverrunTrailer_MultiSegment verifies overruns across segment rotation.
func TestOverrunTrailer_MultiSegment(t *testing.T) {
	const numCh = 2
	frameSize := numCh * 4

	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")
	segSize := frameSize * 5 // Very small segments.
	w, err := NewWriter(dir, h, WithFileSize(segSize))
	require.NoError(t, err)

	// Write 20 frames with overruns at various points.
	for i := range 20 {
		w.WriteBulk(framesToBulk([][]uint32{{uint32(i), uint32(i + 100)}}))
		if i == 3 || i == 12 {
			w.RecordOverrun(OverrunEvent{FrameIndex: uint64(i), Timestamp: int64(1700000000000 + i*1000)})
		}
	}
	require.NoError(t, w.Close())

	r, err := NewReader(dir)
	require.NoError(t, err)
	defer r.Close()

	rh := r.Header()
	require.Equal(t, uint64(20), rh.FrameCount)
	require.Len(t, rh.Overruns, 2)
	require.Equal(t, uint64(3), rh.Overruns[0].FrameIndex)
	require.Equal(t, uint64(12), rh.Overruns[1].FrameIndex)
}

// TestOverrunLog verifies the sidecar JSONL file.
func TestOverrunLog(t *testing.T) {
	dir := t.TempDir()
	ol, err := NewOverrunLog(dir)
	require.NoError(t, err)

	require.Equal(t, 0, ol.Count())

	require.NoError(t, ol.Record(OverrunEvent{FrameIndex: 100, Timestamp: 1700000000000}))
	require.NoError(t, ol.Record(OverrunEvent{FrameIndex: 200, Timestamp: 1700000001000}))

	require.Equal(t, 2, ol.Count())

	events := ol.Events()
	require.Len(t, events, 2)
	require.Equal(t, uint64(100), events[0].FrameIndex)

	// Close should keep the file (has events).
	require.NoError(t, ol.Close())
	_, err = os.Stat(filepath.Join(dir, "overruns.jsonl"))
	require.NoError(t, err, "sidecar should exist after close with events")
}

// TestOverrunLog_EmptyRemoved verifies the sidecar is removed when empty.
func TestOverrunLog_EmptyRemoved(t *testing.T) {
	dir := t.TempDir()
	ol, err := NewOverrunLog(dir)
	require.NoError(t, err)
	require.NoError(t, ol.Close())

	_, err = os.Stat(filepath.Join(dir, "overruns.jsonl"))
	require.True(t, os.IsNotExist(err), "empty sidecar should be removed on close")
}

// TestSegmentInfo_HasOverruns verifies the HasOverruns field in SegmentInfo.
func TestSegmentInfo_HasOverruns(t *testing.T) {
	const numCh = 2
	h := testHeaderRaw(numCh)
	dir := filepath.Join(t.TempDir(), "capture")

	w, err := NewWriter(dir, h)
	require.NoError(t, err)
	w.WriteBulk(framesToBulk([][]uint32{{1, 2}}))
	w.RecordOverrun(OverrunEvent{FrameIndex: 0, Timestamp: 1700000000000})
	w.Close()

	info, err := ReadSegmentInfo(filepath.Join(dir, "seg_0000.daq"))
	require.NoError(t, err)
	require.True(t, info.HasOverruns)
}

func benchData(numCh, numFrames int) [][]uint32 {
	data := make([][]uint32, numFrames)
	for i := range numFrames {
		vals := make([]uint32, numCh)
		for ch := range numCh {
			vals[ch] = uint32(device.ADCMidscale + (i*13+ch*7)%1000)
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
		dir := filepath.Join(b.TempDir(), "capture")
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

	dir := filepath.Join(b.TempDir(), "capture")
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
