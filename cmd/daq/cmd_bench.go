package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808"
	"github.com/borud/mcc-usb-1808/capture"
)

type benchCmd struct {
	Channels string  `help:"Analog input channels." default:"0-7"`
	Rate     float64 `help:"Sample rate in Hz per channel." default:"35000"`
	Duration float64 `help:"Test duration in seconds." default:"5"`
}

func (b *benchCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	queue, err := parseChannels(b.Channels)
	if err != nil {
		return err
	}

	rng, err := parseRanges("bp10v", max(len(queue), 1))
	if err != nil {
		return err
	}

	var analogChannels []int
	for _, ch := range queue {
		if ch < usb1808.NumAInChannels {
			analogChannels = append(analogChannels, ch)
		}
	}

	mode, _ := parseMode("differential")
	if len(analogChannels) > 0 {
		if err := configureAnalogInputs(dev, analogChannels, rng, mode); err != nil {
			return err
		}
	}

	_ = dev.StopAnalogInScan()

	// Simulate capture's buildHeader (device reads between stop and scan start).
	_, _ = dev.SerialNumber()
	_, _, _ = dev.FPGAVersion()
	_, _ = dev.CalibrationDate()
	_ = dev.AnalogInCalTable()

	nCh := len(queue)
	cfg := usb1808.AnalogInScanConfig{
		Channels: queue,
		Rate:     b.Rate,
		Count:    0,
	}

	xferMode := "sync-multi-reader"
	if dev.AsyncBulkSupported() {
		xferMode = "async-ring"
	}
	fmt.Fprintf(os.Stderr, "bench: %d ch, %.0f Hz/ch, %.0fs [%s]\n",
		nCh, b.Rate, b.Duration, xferMode)

	// Simulate capture command: signal handler, file, writer.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sigCh
		cancel()
		signal.Reset(os.Interrupt)
	}()

	// Timer-based cancel for bench duration.
	go func() {
		time.Sleep(time.Duration(b.Duration * float64(time.Second)))
		cancel()
	}()

	f, err := os.Create("/dev/null")
	if err != nil {
		return err
	}
	defer f.Close()

	h := capture.Header{
		Channels:   make([]capture.Channel, nCh),
		SampleRate: b.Rate,
		Format:     capture.RawUint32,
	}
	for i := range nCh {
		h.Channels[i] = capture.Channel{Index: queue[i], Type: capture.AnalogIn}
	}
	cw, err := capture.NewWriter(f, h, capture.WithBufferSize(8192))
	if err != nil {
		return err
	}
	defer cw.Close()

	// Replicate capture's write goroutine pattern.
	const writeDepth = 64
	writeCh := make(chan []byte, writeDepth)
	writePool := make(chan []byte, writeDepth+1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for data := range writeCh {
			_ = cw.WriteBulk(data)
			writePool <- data[:cap(data)]
		}
	}()

	var reads int
	var totalBytes int64
	start := time.Now()
	var maxGap time.Duration
	lastRead := start

	for data, err := range dev.ScanAnalogInBulk(ctx, cfg) {
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			elapsed := time.Since(start)
			fmt.Fprintf(os.Stderr, "FAILED after %d reads, %d bytes, %.2fs: %v\n",
				reads, totalBytes, elapsed.Seconds(), err)
			fmt.Fprintf(os.Stderr, "  max gap between frames: %v\n", maxGap)
			close(writeCh)
			wg.Wait()
			return err
		}
		var buf []byte
		select {
		case buf = <-writePool:
		default:
			buf = make([]byte, cap(data))
		}
		buf = buf[:len(data)]
		copy(buf, data)
		writeCh <- buf
		now := time.Now()
		gap := now.Sub(lastRead)
		if gap > maxGap {
			maxGap = gap
		}
		lastRead = now
		reads++
		totalBytes += int64(len(data))
	}

	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "OK: %d frames, %.1f MB, %.2fs, %.1f MB/s, max_gap=%v\n",
		reads, float64(totalBytes)/1e6, elapsed.Seconds(),
		float64(totalBytes)/elapsed.Seconds()/1e6, maxGap)
	return nil
}
