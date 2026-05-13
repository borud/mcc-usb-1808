package capture

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
)

const defaultBufferFrames = 1024

type segmentWriter struct {
	f            *os.File
	pendingPath  string
	finalPath    string
	renamed      bool
	frameSize    int
	buf          []byte
	bufUsed      int
	framesWritten uint64
	seq          uint16
	globalOffset uint64
	closed       bool
}

func newSegmentWriter(pendingPath, finalPath string, h Header, seq uint16, globalOffset uint64, bufferFrames int) (*segmentWriter, error) {
	if len(h.Channels) == 0 {
		return nil, ErrNoChannels
	}
	if h.Format != RawUint32 {
		return nil, ErrInvalidFormat
	}

	const sampleBytes = 4
	frameSize := len(h.Channels) * sampleBytes

	headerJSON, err := json.Marshal(h)
	if err != nil {
		return nil, err
	}

	f, err := os.Create(pendingPath) // #nosec G304 -- path constructed internally from segment sequence number
	if err != nil {
		return nil, err
	}

	if err := writePreamble(f, seq, globalOffset, headerJSON); err != nil {
		f.Close()
		os.Remove(pendingPath)
		return nil, err
	}

	if bufferFrames <= 0 {
		bufferFrames = defaultBufferFrames
	}

	return &segmentWriter{
		f:            f,
		pendingPath:  pendingPath,
		finalPath:    finalPath,
		frameSize:    frameSize,
		buf:          make([]byte, bufferFrames*frameSize),
		seq:          seq,
		globalOffset: globalOffset,
	}, nil
}

func (sw *segmentWriter) ensureRenamed() error {
	if sw.renamed {
		return nil
	}
	if err := os.Rename(sw.pendingPath, sw.finalPath); err != nil {
		return err
	}
	sw.renamed = true
	return nil
}

// patchGlobalOffset updates the global frame offset in the preamble.
// Called before first write when the actual offset is known.
func (sw *segmentWriter) patchGlobalOffset(offset uint64) error {
	if sw.globalOffset == offset {
		return nil
	}
	sw.globalOffset = offset
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], offset)
	if _, err := sw.f.Seek(globalFrameOffsetOff, io.SeekStart); err != nil {
		return err
	}
	if _, err := sw.f.Write(buf[:]); err != nil {
		return err
	}
	// Seek back to end for data writes.
	_, err := sw.f.Seek(0, io.SeekEnd)
	return err
}

func (sw *segmentWriter) writeBulk(data []byte) error {
	if sw.closed {
		return ErrWriterClosed
	}

	if len(data) == 0 {
		return nil
	}

	if err := sw.ensureRenamed(); err != nil {
		return err
	}

	sw.framesWritten += uint64(len(data) / sw.frameSize)
	for len(data) > 0 {
		space := len(sw.buf) - sw.bufUsed
		if space == 0 {
			if err := sw.flush(); err != nil {
				return err
			}
			space = len(sw.buf)
		}
		n := min(len(data), space)
		copy(sw.buf[sw.bufUsed:], data[:n])
		sw.bufUsed += n
		data = data[n:]
	}
	return nil
}

func (sw *segmentWriter) flush() error {
	if sw.bufUsed == 0 {
		return nil
	}
	_, err := sw.f.Write(sw.buf[:sw.bufUsed])
	sw.bufUsed = 0
	return err
}

// closeKeep closes the segment and keeps it even if empty. Used for the
// active segment which should always be retained.
func (sw *segmentWriter) closeKeep() error {
	return sw.doClose(true)
}

// closeRemoveIfEmpty closes the segment and removes it if no frames were written.
// Used for pre-opened "next" segments that were never needed.
func (sw *segmentWriter) closeRemoveIfEmpty() error {
	return sw.doClose(false)
}

func (sw *segmentWriter) doClose(keep bool) error {
	if sw.closed {
		return ErrWriterClosed
	}
	sw.closed = true

	if sw.framesWritten == 0 && !keep {
		sw.f.Close()
		os.Remove(sw.pendingPath)
		if sw.renamed {
			os.Remove(sw.finalPath)
		}
		return nil
	}

	// Ensure the file has its final name.
	if !sw.renamed {
		if err := os.Rename(sw.pendingPath, sw.finalPath); err != nil {
			sw.f.Close()
			return err
		}
		sw.renamed = true
	}

	if err := sw.flush(); err != nil {
		sw.f.Close()
		return err
	}

	// Patch the frame count in the preamble.
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], sw.framesWritten)
	if _, err := sw.f.Seek(frameCountOff, io.SeekStart); err != nil {
		sw.f.Close()
		return err
	}
	if _, err := sw.f.Write(buf[:]); err != nil {
		sw.f.Close()
		return err
	}

	return sw.f.Close()
}
