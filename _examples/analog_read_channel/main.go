// Example analog_read_channel demonstrates what happens when a consumer
// cannot keep up with the scan rate.
//
// At 200 kS/s × 8 channels the device produces 6.4 MB/s of raw data.
// Terminal output via fmt.Printf can typically sustain 10–50 k lines/s
// — roughly 10–20× too slow.  This mismatch is the central problem in
// high-rate DAQ: the producer (hardware) is clock-driven and does not
// wait for the consumer.
//
// # Why the goroutine matters
//
// scan.Chunks() is backed by an internal pipeline of USB reader
// goroutines.  If the caller of Chunks() blocks (e.g. on printf),
// backpressure propagates through the pipeline all the way to the USB
// bulk reads.  While the readers are stalled, the on-device FIFO fills
// up and eventually overruns, losing data permanently.
//
// By draining Chunks() in a dedicated goroutine and forwarding into a
// buffered channel, we decouple the USB readers from the slow consumer.
// The buffered channel absorbs short bursts, and the select/default
// pattern drops chunks when the buffer is full instead of stalling the
// readers.
//
// # Expected output
//
// The program will print a subset of the captured frames (those the
// consumer managed to process in time) and then report how many chunks
// were dropped.  A typical run might show:
//
//	INFO channel full counter full=113
//
// This means 113 chunks (~2048 frames each) were silently discarded
// because the consumer could not keep up.
//
// # Takeaway
//
// For real applications, do minimal work inside the scan loop — copy
// raw data into a ring buffer, write to a file, or accumulate into
// memory — and defer expensive processing (formatting, analysis) until
// after the scan completes.  See the analog_read example for a pattern
// that captures all frames without loss.
package main

import (
	"fmt"
	"log"
	"log/slog"
	"sync/atomic"

	"github.com/borud/mcc-usb-1808/v4/codec"
	"github.com/borud/mcc-usb-1808/v4/device"
)

func main() {
	dev, err := device.Open()
	if err != nil {
		log.Fatal(err)
	}

	channelConfig := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 2, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 3, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 4, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 5, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 6, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 7, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
	}

	scanConfig := device.ScanConfig{
		Channels: channelConfig,
		Rate:     200_000,
		Count:    100_000,
	}

	decoder, err := codec.NewDecoder(channelConfig, dev.CalibrationTable())
	if err != nil {
		log.Fatal(err)
	}

	scan, err := dev.CreateScan(scanConfig)
	if err != nil {
		log.Fatal(err)
	}

	if err := scan.Start(); err != nil {
		log.Fatal(err)
	}

	// Drain Chunks() in its own goroutine so the USB readers are never
	// stalled by the slow consumer.  The buffered channel provides a
	// small cushion; when it fills, chunks are dropped rather than
	// creating backpressure that would overrun the device FIFO.
	//
	// The channel below is too small to keep up, thus triggering drops.
	chunkCh := make(chan device.Chunk, 10)
	var dropped atomic.Uint64

	go func() {
		defer close(chunkCh)
		for chunk := range scan.Chunks() {
			select {
			case chunkCh <- chunk:
			default:
				dropped.Add(1)
			}
		}
	}()

	// This loop is deliberately slow (printf per frame) to demonstrate
	// the effect of a consumer that cannot keep up.
	frameCount := 0
	for chunk := range chunkCh {
		for frame := range decoder.Frames(chunk.Data) {
			fmt.Printf("%9d ", frameCount)
			for i := range len(channelConfig) {
				fmt.Printf("|  %d : %3.4f", i, frame.Voltage(i))
			}
			frameCount++
			fmt.Println()
		}
	}

	scan.Stop()

	slog.Info("scan complete",
		"frames_printed", frameCount,
		"chunks_dropped", dropped.Load(),
		"frames_requested", scanConfig.Count,
	)
}
