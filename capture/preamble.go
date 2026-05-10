package capture

import (
	"encoding/binary"
	"encoding/json"
	"io"
)

type preamble struct {
	flags      uint8
	headerLen  uint32
	frameCount uint64
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
		flags:      buf[5],
		headerLen:  binary.LittleEndian.Uint32(buf[6:]),
		frameCount: binary.LittleEndian.Uint64(buf[10:]),
	}, nil
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
	case CalibratedFloat64:
		return 8, nil
	default:
		return 0, ErrInvalidFormat
	}
}
