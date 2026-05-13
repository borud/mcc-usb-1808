package capture

import (
	"encoding/binary"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type segmentInfo struct {
	seq               uint16
	globalFrameOffset uint64
	frameCount        uint64
	dataStartBytes    int64
	path              string
}

// Reader reads segmented capture data from a directory.
//
// Frames are read sequentially across segment boundaries via [Reader.ReadFrame]
// or [Reader.Frames], or randomly via [Reader.ReadFrames]. Internal buffers
// are reused across calls for zero per-frame allocation; callers must copy
// data they need to retain.
//
// Reader is NOT safe for concurrent use.
type Reader struct {
	header    Header
	segments  []segmentInfo
	frameSize int
	numCh     int

	// Sequential read state.
	curSegIdx int
	curFile   *os.File
	buf       []byte
	frame     Frame
	seqDone   bool

	// Random-access state.
	raBuf    []byte
	raFrames []Frame

	closed bool
}

// NewReader opens a capture directory and returns a Reader positioned at
// the first frame. If segments are specified, only those segment numbers
// are included; otherwise all segments are read.
//
// Returns [ErrNotDirectory] if dir is not a directory.
// Returns [ErrNoSegments] if no segment files are found.
// Returns [ErrSegmentMismatch] if segment headers are inconsistent.
func NewReader(dir string, segments ...int) (*Reader, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotDirectory, dir)
	}

	segs, headerJSON, err := discoverSegments(dir)
	if err != nil {
		return nil, err
	}

	// Filter segments if specific numbers requested.
	if len(segments) > 0 {
		wanted := make(map[int]bool, len(segments))
		for _, s := range segments {
			wanted[s] = true
		}
		filtered := segs[:0]
		for _, seg := range segs {
			if wanted[int(seg.seq)] {
				filtered = append(filtered, seg)
			}
		}
		if len(filtered) == 0 {
			return nil, ErrNoSegments
		}
		segs = filtered
	}

	// Parse header from first segment.
	h, err := parseHeaderFromSegment(segs[0].path)
	if err != nil {
		return nil, err
	}

	// Compute aggregate frame count.
	var totalFrames uint64
	for _, seg := range segs {
		totalFrames += seg.frameCount
	}
	h.FrameCount = totalFrames

	ss, err := sampleSize(h.Format)
	if err != nil {
		return nil, err
	}
	numCh := len(h.Channels)
	frameSize := numCh * ss

	// Verify all segments have consistent headers.
	for i := 1; i < len(segs); i++ {
		otherJSON, err := readHeaderJSONBytes(segs[i].path)
		if err != nil {
			return nil, err
		}
		if string(otherJSON) != string(headerJSON) {
			return nil, fmt.Errorf("%w: segment %d differs from segment %d", ErrSegmentMismatch, segs[i].seq, segs[0].seq)
		}
	}

	frame := Frame{header: &h}
	switch h.Format {
	case RawUint32:
		frame.raw = make([]uint32, numCh)
		frame.floats = make([]float64, numCh)
	case calibratedFloat64:
		frame.floats = make([]float64, numCh)
	}

	return &Reader{
		header:    h,
		segments:  segs,
		frameSize: frameSize,
		numCh:     numCh,
		curSegIdx: -1, // will be opened on first ReadFrame
		buf:       make([]byte, frameSize),
		frame:     frame,
	}, nil
}

// Header returns the capture header with aggregate FrameCount across all
// segments.
func (rd *Reader) Header() Header {
	return rd.header
}

// ReadFrame reads the next frame from the capture. Iteration is seamless
// across segment boundaries. Returns [io.EOF] when all segments are
// exhausted.
//
// The returned [Frame] and its slices are valid until the next call to
// ReadFrame. Returns [ErrReaderClosed] if the reader has been closed.
func (rd *Reader) ReadFrame() (*Frame, error) {
	if rd.closed {
		return nil, ErrReaderClosed
	}
	if rd.seqDone {
		return nil, io.EOF
	}

	for {
		// Open first/next segment if needed.
		if rd.curFile == nil {
			rd.curSegIdx++
			if rd.curSegIdx >= len(rd.segments) {
				rd.seqDone = true
				return nil, io.EOF
			}
			f, err := os.Open(rd.segments[rd.curSegIdx].path)
			if err != nil {
				return nil, err
			}
			// Seek past preamble + header to frame data.
			if _, err := f.Seek(rd.segments[rd.curSegIdx].dataStartBytes, io.SeekStart); err != nil {
				f.Close()
				return nil, err
			}
			rd.curFile = f
		}

		_, err := io.ReadFull(rd.curFile, rd.buf)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			rd.curFile.Close()
			rd.curFile = nil
			if err == io.ErrUnexpectedEOF {
				return nil, err
			}
			continue // try next segment
		}
		if err != nil {
			return nil, err
		}

		rd.decodeFrame()
		return &rd.frame, nil
	}
}

// Frames returns an iterator over all remaining frames. Iteration stops
// at [io.EOF]. Any other error is yielded to the caller and iteration stops.
func (rd *Reader) Frames() iter.Seq2[*Frame, error] {
	return func(yield func(*Frame, error) bool) {
		for {
			frame, err := rd.ReadFrame()
			if err == io.EOF {
				return
			}
			if !yield(frame, err) {
				return
			}
			if err != nil {
				return
			}
		}
	}
}

// ReadFrames reads up to n frames starting at global frame index offset.
// Uses the segment index and global frame offsets for efficient seeking.
//
// Returns the frames read and any error. If offset is beyond the end,
// returns nil and nil. The returned slice is owned by the Reader and
// reused on the next call.
func (rd *Reader) ReadFrames(offset uint64, n int) ([]Frame, error) {
	if rd.closed {
		return nil, ErrReaderClosed
	}
	if n <= 0 {
		return nil, nil
	}

	// Clamp to total frames if known.
	if rd.header.FrameCount > 0 {
		if offset >= rd.header.FrameCount {
			return nil, nil
		}
		if avail := rd.header.FrameCount - offset; uint64(n) > avail {
			n = int(avail)
		}
	}

	// Find the starting segment via binary search.
	segIdx := sort.Search(len(rd.segments), func(i int) bool {
		seg := rd.segments[i]
		return seg.globalFrameOffset+seg.frameCount > offset
	})
	if segIdx >= len(rd.segments) {
		return nil, nil
	}

	rd.ensureRABuffers(n)

	read := 0
	for read < n && segIdx < len(rd.segments) {
		seg := rd.segments[segIdx]
		localOffset := offset + uint64(read) - seg.globalFrameOffset
		localAvail := seg.frameCount - localOffset
		want := uint64(n - read)
		if want > localAvail {
			want = localAvail
		}

		f, err := os.Open(seg.path)
		if err != nil {
			if read > 0 {
				return rd.raFrames[:read], nil
			}
			return nil, err
		}

		seekPos := seg.dataStartBytes + int64(localOffset)*int64(rd.frameSize)
		if _, err := f.Seek(seekPos, io.SeekStart); err != nil {
			f.Close()
			if read > 0 {
				return rd.raFrames[:read], nil
			}
			return nil, err
		}

		totalBytes := int(want) * rd.frameSize
		nn, err := io.ReadFull(f, rd.raBuf[:totalBytes])
		f.Close()

		framesRead := nn / rd.frameSize
		for i := range framesRead {
			rd.decodeRAFrame(read+i, i)
		}
		read += framesRead

		if err != nil && framesRead == 0 {
			if read > 0 {
				return rd.raFrames[:read], nil
			}
			return nil, err
		}

		segIdx++
	}

	return rd.raFrames[:read], nil
}

// Close releases resources held by the Reader.
func (rd *Reader) Close() error {
	if rd.closed {
		return ErrReaderClosed
	}
	rd.closed = true
	if rd.curFile != nil {
		rd.curFile.Close()
		rd.curFile = nil
	}
	return nil
}

func (rd *Reader) decodeFrame() {
	switch rd.header.Format {
	case RawUint32:
		for i := range rd.numCh {
			rd.frame.raw[i] = binary.LittleEndian.Uint32(rd.buf[i*4:])
		}
	case calibratedFloat64:
		for i := range rd.numCh {
			rd.frame.floats[i] = math.Float64frombits(binary.LittleEndian.Uint64(rd.buf[i*8:]))
		}
	}
}

func (rd *Reader) ensureRABuffers(n int) {
	for len(rd.raFrames) < n {
		f := Frame{header: &rd.header}
		switch rd.header.Format {
		case RawUint32:
			f.raw = make([]uint32, rd.numCh)
			f.floats = make([]float64, rd.numCh)
		case calibratedFloat64:
			f.floats = make([]float64, rd.numCh)
		}
		rd.raFrames = append(rd.raFrames, f)
	}

	needed := n * rd.frameSize
	if len(rd.raBuf) < needed {
		rd.raBuf = make([]byte, needed)
	}
}

func (rd *Reader) decodeRAFrame(frameIdx, bufIdx int) {
	off := bufIdx * rd.frameSize
	switch rd.header.Format {
	case RawUint32:
		for ch := range rd.numCh {
			rd.raFrames[frameIdx].raw[ch] = binary.LittleEndian.Uint32(rd.raBuf[off+ch*4:])
		}
	case calibratedFloat64:
		for ch := range rd.numCh {
			rd.raFrames[frameIdx].floats[ch] = math.Float64frombits(binary.LittleEndian.Uint64(rd.raBuf[off+ch*8:]))
		}
	}
}

// discoverSegments finds and parses all segment files in dir.
// Returns the sorted segment list and the raw header JSON from the first segment.
func discoverSegments(dir string) ([]segmentInfo, []byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}

	var segs []segmentInfo
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "seg_") || !strings.HasSuffix(name, ".daq") {
			continue
		}
		// Skip pending files.
		if strings.HasSuffix(name, ".pending") {
			continue
		}

		path := filepath.Join(dir, name)
		pre, err := readPreambleFromFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("segment %s: %w", name, err)
		}

		segs = append(segs, segmentInfo{
			seq:               pre.sequenceNumber,
			globalFrameOffset: pre.globalFrameOffset,
			frameCount:        pre.frameCount,
			dataStartBytes:    int64(preambleLen) + int64(pre.headerLen),
			path:              path,
		})
	}

	if len(segs) == 0 {
		return nil, nil, ErrNoSegments
	}

	sort.Slice(segs, func(i, j int) bool {
		return segs[i].seq < segs[j].seq
	})

	// Read header JSON from first segment for consistency checking.
	headerJSON, err := readHeaderJSONBytes(segs[0].path)
	if err != nil {
		return nil, nil, err
	}

	return segs, headerJSON, nil
}

func readPreambleFromFile(path string) (preamble, error) {
	f, err := os.Open(path) // #nosec G304 -- path constructed from directory listing
	if err != nil {
		return preamble{}, err
	}
	defer f.Close()
	return readPreamble(f)
}

func readHeaderJSONBytes(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- path constructed from directory listing
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pre, err := readPreamble(f)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, pre.headerLen)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func parseHeaderFromSegment(path string) (Header, error) {
	f, err := os.Open(path) // #nosec G304 -- path constructed from directory listing
	if err != nil {
		return Header{}, err
	}
	defer f.Close()

	pre, err := readPreamble(f)
	if err != nil {
		return Header{}, err
	}

	h, err := readHeaderJSON(f, pre.headerLen)
	if err != nil {
		return Header{}, err
	}
	h.FrameCount = pre.frameCount
	return h, nil
}
