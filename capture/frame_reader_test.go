package capture

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func writeSeekableRaw(t *testing.T, numCh, numFrames int) (*os.File, [][]uint32) {
	t.Helper()

	f, err := os.CreateTemp("", "frame-reader-*.daq")
	if err != nil {
		t.Fatal(err)
	}

	h := testHeaderRaw(numCh)
	w, err := NewWriter(f, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeRawFrames(t, w, numCh, numFrames)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen for reading.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return f, written
}

func writeSeekableFloat64(t *testing.T, numCh, numFrames int) (*os.File, [][]float64) {
	t.Helper()

	f, err := os.CreateTemp("", "frame-reader-*.daq")
	if err != nil {
		t.Fatal(err)
	}

	h := testHeaderFloat64(numCh)
	w, err := NewWriter(f, h)
	if err != nil {
		t.Fatal(err)
	}
	written := writeFloat64Frames(t, w, numCh, numFrames)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return f, written
}

func TestFrameReader_RoundTrip_Raw(t *testing.T) {
	const numCh = 4
	const numFrames = 100

	f, written := writeSeekableRaw(t, numCh, numFrames)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()

	if fr.FrameCount() != numFrames {
		t.Fatalf("FrameCount = %d, want %d", fr.FrameCount(), numFrames)
	}

	frames, err := fr.ReadFrames(0, numFrames)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != numFrames {
		t.Fatalf("got %d frames, want %d", len(frames), numFrames)
	}

	for i, frame := range frames {
		raw := frame.RawValues()
		for ch := range numCh {
			if raw[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", i, ch, raw[ch], written[i][ch])
			}
		}
	}
}

func TestFrameReader_RoundTrip_Float64(t *testing.T) {
	const numCh = 3
	const numFrames = 50

	f, written := writeSeekableFloat64(t, numCh, numFrames)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()

	frames, err := fr.ReadFrames(0, numFrames)
	if err != nil {
		t.Fatal(err)
	}

	for i, frame := range frames {
		vals := frame.Values()
		for ch := range numCh {
			if vals[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d: got %f, want %f", i, ch, vals[ch], written[i][ch])
			}
		}
	}
}

func TestFrameReader_RandomAccess(t *testing.T) {
	const numCh = 2
	const numFrames = 200

	f, written := writeSeekableRaw(t, numCh, numFrames)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()

	// Read from the middle.
	frames, err := fr.ReadFrames(50, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 10 {
		t.Fatalf("got %d frames, want 10", len(frames))
	}
	for i, frame := range frames {
		raw := frame.RawValues()
		for ch := range numCh {
			if raw[ch] != written[50+i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", 50+i, ch, raw[ch], written[50+i][ch])
			}
		}
	}

	// Read from the end.
	frames, err = fr.ReadFrames(190, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 10 {
		t.Fatalf("got %d frames, want 10", len(frames))
	}
	for i, frame := range frames {
		raw := frame.RawValues()
		for ch := range numCh {
			if raw[ch] != written[190+i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", 190+i, ch, raw[ch], written[190+i][ch])
			}
		}
	}

	// Seek back to the beginning.
	frames, err = fr.ReadFrames(0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 5 {
		t.Fatalf("got %d frames, want 5", len(frames))
	}
	for i, frame := range frames {
		raw := frame.RawValues()
		for ch := range numCh {
			if raw[ch] != written[i][ch] {
				t.Errorf("frame %d ch %d: got %d, want %d", i, ch, raw[ch], written[i][ch])
			}
		}
	}
}

func TestFrameReader_OutOfBounds(t *testing.T) {
	const numCh = 2
	const numFrames = 10

	f, _ := writeSeekableRaw(t, numCh, numFrames)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()

	// Offset past end.
	frames, err := fr.ReadFrames(100, 5)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if frames != nil {
		t.Fatalf("expected nil frames, got %d", len(frames))
	}

	// Partial read (request more than available).
	frames, err = fr.ReadFrames(7, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
}

func TestFrameReader_CompressedRejected(t *testing.T) {
	var buf bytes.Buffer
	h := testHeaderRaw(2)
	w, err := NewWriter(&buf, h, WithCompression(true))
	if err != nil {
		t.Fatal(err)
	}
	w.WriteFrame([]uint32{1, 2})
	w.Close()

	_, err = NewFrameReader(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrCompressedSeek) {
		t.Fatalf("expected ErrCompressedSeek, got %v", err)
	}
}

func TestFrameReader_AfterClose(t *testing.T) {
	f, _ := writeSeekableRaw(t, 2, 5)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	fr.Close()

	_, err = fr.ReadFrames(0, 1)
	if !errors.Is(err, ErrReaderClosed) {
		t.Fatalf("expected ErrReaderClosed, got %v", err)
	}
}

func TestFrameReader_DoubleClose(t *testing.T) {
	f, _ := writeSeekableRaw(t, 2, 5)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fr.Close(); !errors.Is(err, ErrReaderClosed) {
		t.Fatalf("expected ErrReaderClosed on double close, got %v", err)
	}
}

func TestFrameReader_ZeroN(t *testing.T) {
	f, _ := writeSeekableRaw(t, 2, 5)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()

	frames, err := fr.ReadFrames(0, 0)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if frames != nil {
		t.Fatalf("expected nil frames, got %d", len(frames))
	}
}

func TestFrameReader_Duration(t *testing.T) {
	f, _ := writeSeekableRaw(t, 2, 1000)
	defer os.Remove(f.Name())
	defer f.Close()

	fr, err := NewFrameReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()

	// 1000 frames at 1000 Hz = 1 second.
	want := time.Second
	if fr.Duration() != want {
		t.Errorf("Duration = %v, want %v", fr.Duration(), want)
	}
}

func TestHeader_FrameAtTime(t *testing.T) {
	h := Header{SampleRate: 1000}

	if got := h.FrameAtTime(0); got != 0 {
		t.Errorf("FrameAtTime(0) = %d, want 0", got)
	}
	if got := h.FrameAtTime(1.0); got != 1000 {
		t.Errorf("FrameAtTime(1.0) = %d, want 1000", got)
	}
	if got := h.FrameAtTime(0.5); got != 500 {
		t.Errorf("FrameAtTime(0.5) = %d, want 500", got)
	}
	if got := h.FrameAtTime(-1.0); got != 0 {
		t.Errorf("FrameAtTime(-1.0) = %d, want 0", got)
	}
}

func TestHeader_TimeAtFrame(t *testing.T) {
	h := Header{SampleRate: 1000}

	if got := h.TimeAtFrame(0); got != 0 {
		t.Errorf("TimeAtFrame(0) = %f, want 0", got)
	}
	if got := h.TimeAtFrame(1000); got != 1.0 {
		t.Errorf("TimeAtFrame(1000) = %f, want 1.0", got)
	}
	if got := h.TimeAtFrame(500); got != 0.5 {
		t.Errorf("TimeAtFrame(500) = %f, want 0.5", got)
	}
}

func TestHeader_FrameAtTime_TimeAtFrame_Roundtrip(t *testing.T) {
	h := Header{SampleRate: 48000}

	for _, seconds := range []float64{0, 0.001, 1.0, 10.5, 60.0} {
		idx := h.FrameAtTime(seconds)
		got := h.TimeAtFrame(idx)
		// Allow truncation error of one sample period.
		if diff := seconds - got; diff < 0 || diff >= 1.0/h.SampleRate {
			t.Errorf("roundtrip(%f): FrameAtTime=%d, TimeAtFrame=%f, diff=%f",
				seconds, idx, got, diff)
		}
	}
}

func TestHeader_Duration(t *testing.T) {
	h := Header{SampleRate: 1000, FrameCount: 5000}
	want := 5 * time.Second
	if got := h.Duration(); got != want {
		t.Errorf("Duration = %v, want %v", got, want)
	}

	h.FrameCount = 0
	if got := h.Duration(); got != 0 {
		t.Errorf("Duration with unknown FrameCount = %v, want 0", got)
	}
}
