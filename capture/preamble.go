package capture

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

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

	return preamble{
		headerLen:         binary.LittleEndian.Uint32(buf[6:]),
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

func sampleSize(f DataFormat) (int, error) {
	switch f {
	case RawUint32:
		return 4, nil
	case calibratedFloat64:
		return 8, nil
	default:
		return 0, ErrInvalidFormat
	}
}
