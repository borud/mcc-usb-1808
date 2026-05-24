// stream_aout demonstrates AOutWriter: streaming raw uint16 LE samples from a
// file to DAC0. Run with a file of little-endian uint16 DAC codes, e.g.:
//
//	go run . --file waveform.raw --rate 100000
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"

	"github.com/borud/mcc-usb-1808/v4/device"
)

func main() {
	var (
		filePath = "waveform.raw"
		rate     = 100_000
	)

	// Simple flag parsing; use kong for real CLI tools.
	args := os.Args[1:]
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--file":
			filePath = args[i+1]
			i++
		case "--rate":
			if _, err := fmt.Sscanf(args[i+1], "%d", &rate); err != nil {
				log.Fatalf("invalid rate: %v", err)
			}
			i++
		}
	}

	slog.Info("stream_aout starting", "file", filePath, "rate", rate) // #nosec G706 -- example; values from user args, not user-controlled log keys

	dev, err := device.Open()
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer dev.Close()

	if err := dev.Init(); err != nil {
		log.Fatalf("init: %v", err)
	}

	f, err := os.Open(filePath) // #nosec G703 -- example program, path from user args
	if err != nil {
		log.Fatalf("open file: %v", err)
	}
	defer f.Close()

	cfg := device.AOutScanConfig{
		Channels: []int{0},
		Rate:     rate,
		Count:    0, // continuous; Close drains the FIFO
	}

	w, err := dev.NewAOutWriter(cfg)
	if err != nil {
		log.Fatalf("NewAOutWriter: %v", err)
	}
	defer w.Close()

	if err := w.Start(); err != nil {
		log.Fatalf("Start: %v", err)
	}

	buf := make([]uint16, 2048)
	total := 0

	for {
		n, err := readUint16LE(f, buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				log.Fatalf("Write: %v", werr)
			}
			total += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("read file: %v", err)
		}
	}

	slog.Info("stream complete", "samples", total) // #nosec G706 -- example; integer value, not tainted string
}

// readUint16LE reads up to len(buf) uint16 values from r in little-endian order.
func readUint16LE(r io.Reader, buf []uint16) (int, error) {
	raw := make([]byte, len(buf)*2)
	n, err := io.ReadFull(r, raw)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	samples := n / 2
	for i := range samples {
		buf[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return samples, err
}
