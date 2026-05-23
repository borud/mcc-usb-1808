package capture

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// maxHeaderLen is the maximum allowed header size (1 MiB).
const maxHeaderLen = 1 << 20

var errReservedFlags = errors.New("capture: reserved flags byte is non-zero")

type preamble struct {
	headerLen         uint32
	frameCount        uint64
	sequenceNumber    uint16
	globalFrameOffset uint64
}

func readPreamble(r io.Reader) (preamble, error) {
	buf := make([]byte, preambleLen)
	if _, err := io.ReadFull(r, buf); err != nil {
		return preamble{}, err
	}

	if string(buf[:4]) != fileMagic {
		return preamble{}, ErrInvalidMagic
	}
	if buf[4] != fileVersion {
		return preamble{}, ErrUnsupportedVersion
	}
	if buf[5] != 0 {
		return preamble{}, errReservedFlags
	}

	hLen := binary.LittleEndian.Uint32(buf[6:])
	if hLen > maxHeaderLen {
		return preamble{}, fmt.Errorf("capture: header length %d exceeds maximum %d", hLen, maxHeaderLen)
	}

	return preamble{
		headerLen:         hLen,
		frameCount:        binary.LittleEndian.Uint64(buf[10:]),
		sequenceNumber:    binary.LittleEndian.Uint16(buf[18:]),
		globalFrameOffset: binary.LittleEndian.Uint64(buf[20:]),
	}, nil
}

func writePreamble(w io.Writer, seq uint16, globalOffset uint64, headerJSON []byte) error {
	buf := make([]byte, preambleLen)
	copy(buf, fileMagic)
	buf[4] = fileVersion
	// buf[5] = 0 (flags, reserved)
	binary.LittleEndian.PutUint32(buf[6:], uint32(len(headerJSON)))
	// frame count at offset 10 left as 0; patched at close
	binary.LittleEndian.PutUint16(buf[18:], seq)
	binary.LittleEndian.PutUint64(buf[20:], globalOffset)

	if _, err := w.Write(buf); err != nil {
		return err
	}
	_, err := w.Write(headerJSON)
	return err
}

func readHeaderJSON(r io.Reader, length uint32) (Header, error) {
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Header{}, err
	}

	var h Header
	if err := json.Unmarshal(buf, &h); err != nil {
		return Header{}, err
	}
	return h, nil
}

// SegmentInfo holds summary information from a segment file preamble.
type SegmentInfo struct {
	SequenceNumber    uint16
	FrameCount        uint64
	GlobalFrameOffset uint64
}

// ReadSegmentInfo reads the preamble of a segment file and returns summary info.
func ReadSegmentInfo(path string) (SegmentInfo, error) {
	f, err := os.Open(path) // #nosec G304 -- path from caller
	if err != nil {
		return SegmentInfo{}, err
	}
	defer f.Close()

	pre, err := readPreamble(f)
	if err != nil {
		return SegmentInfo{}, err
	}
	return SegmentInfo{
		SequenceNumber:    pre.sequenceNumber,
		FrameCount:        pre.frameCount,
		GlobalFrameOffset: pre.globalFrameOffset,
	}, nil
}

