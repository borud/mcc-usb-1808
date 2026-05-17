package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808/v4/capture"
	"github.com/borud/mcc-usb-1808/v4/device"
)

type benchCmd struct {
	Channels string  `help:"Channel spec (e.g. ain0-ain7, all)." default:"analog"`
	Rate     string  `help:"Sample rate in Hz per channel (supports k/M suffix)." default:"35k"`
	Duration float64 `help:"Test duration in seconds." default:"5"`
}

func (b *benchCmd) Run(app *cli) error {
	rate, err := parseHumanInt(b.Rate)
	if err != nil {
		return fmt.Errorf("rate: %w", err)
	}

	channels, err := parseChannelSpec(b.Channels, device.BP10V, device.Differential)
	if err != nil {
		return err
	}

	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	cfg := device.ScanConfig{
		Channels: channels,
		Rate:     rate,
		Count:    0,
	}

	handle, err := dev.CreateScan(cfg)
	if err != nil {
		return fmt.Errorf("create scan: %w", err)
	}

	// Simulate capture's buildHeader (device reads between config and scan start).
	_, _ = dev.SerialNumber()
	_, _, _ = dev.FPGAVersion()
	_, _ = dev.CalibrationDate()
	_ = dev.CalibrationTable()

	xferMode := "sync-multi-reader"
	if dev.AsyncBulkSupported() {
		xferMode = "async-ring"
	}
	fmt.Fprintf(os.Stderr, "bench: %d ch, %d Hz/ch, %.0fs [%s]\n",
		len(channels), rate, b.Duration, xferMode)

	// Signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	stopCh := make(chan struct{})
	go func() {
		<-sigCh
		close(stopCh)
		handle.Stop()
		signal.Reset(os.Interrupt)
	}()

	// Timer-based stop.
	go func() {
		timer := time.NewTimer(time.Duration(b.Duration * float64(time.Second)))
		defer timer.Stop()
		select {
		case <-timer.C:
			handle.Stop()
		case <-stopCh:
		}
	}()

	// Temp directory for capture writer (measures real write overhead).
	dir, err := os.MkdirTemp("", "bench-capture-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	nCh := len(channels)
	h := capture.Header{
		Channels:   make([]capture.Channel, nCh),
		SampleRate: float64(rate),
		Format:     capture.RawUint32,
	}
	for i, ch := range channels {
		h.Channels[i] = capture.Channel{Index: ch.Index, Type: capture.AnalogIn}
	}
	cw, err := capture.NewWriter(dir, h, capture.WithBufferSize(8192))
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

	if err := handle.Start(); err != nil {
		return fmt.Errorf("start scan: %w", err)
	}

	var reads int
	var totalBytes int64
	start := time.Now()
	var maxGap time.Duration
	lastRead := start

	for data := range handle.Chunks() {
		var buf []byte
		select {
		case buf = <-writePool:
		default:
			buf = make([]byte, len(data))
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
	close(writeCh)
	wg.Wait()

	if err := handle.Err(); err != nil {
		elapsed := time.Since(start)
		fmt.Fprintf(os.Stderr, "FAILED after %d transfers, %d bytes, %.2fs: %v\n",
			reads, totalBytes, elapsed.Seconds(), err)
		fmt.Fprintf(os.Stderr, "  max gap between transfers: %v\n", maxGap)
		return err
	}

	elapsed := time.Since(start)
	totalSamples := totalBytes / 4
	fmt.Fprintf(os.Stderr, "OK: %d transfers, %d samples, %.1f MB, %.2fs, %.1f MB/s, max_gap=%v\n",
		reads, totalSamples, float64(totalBytes)/1e6, elapsed.Seconds(),
		float64(totalBytes)/elapsed.Seconds()/1e6, maxGap)
	return nil
}
