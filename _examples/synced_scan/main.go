// synced_scan demonstrates simultaneous analog input and output using a shared
// software trigger. DAC0 outputs a 1 kHz sine wave while AIn channels 0–3
// record the response continuously at 100 kS/s until Ctrl+C.
//
// For hardware-synchronized operation (minimal phase skew), replace the
// software trigger with an external pulse connected to the TRIG_IN pin,
// configure both scans with ScanOptExternalTrigger, and call SetTriggerConfig
// before starting either scan. See docs/sync.md for details.
package main

import (
	"context"
	"log"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/borud/mcc-usb-1808/v4/codec"
	"github.com/borud/mcc-usb-1808/v4/device"
)

const (
	rate      = 100_000 // Hz
	sineHz    = 1_000   // Hz
	amplitude = 5.0     // Volts peak
)

func main() {
	slog.Info("synced_scan: AI+AO software-triggered example")

	dev, err := device.Open()
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer dev.Close()

	if err := dev.Init(); err != nil {
		log.Fatalf("init: %v", err)
	}

	// --- Build one period of a 1 kHz sine wave ---
	samplesPerPeriod := rate / sineHz
	sineTable := make([]float64, samplesPerPeriod)
	for i := range samplesPerPeriod {
		sineTable[i] = amplitude * math.Sin(2*math.Pi*float64(i)/float64(samplesPerPeriod))
	}

	// --- Configure AOut: continuous loop on DAC0 ---
	aoutCfg := device.AOutScanConfig{
		Channels: []int{0},
		Rate:     rate,
		Count:    0, // loop forever until Stop
	}
	aout, err := dev.Loop(aoutCfg.Channels, aoutCfg.Rate, sineTable)
	if err != nil {
		log.Fatalf("Loop: %v", err)
	}
	defer aout.Stop()

	slog.Info("AOut running", "rate", rate, "sine_hz", sineHz)

	// --- Configure AIn: record 4 channels continuously ---
	ainChannels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 2, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 3, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
	}
	ainCfg := device.ScanConfig{
		Channels: ainChannels,
		Rate:     rate,
		Count:    0, // continuous until Stop
	}

	decoder, err := codec.NewDecoder(ainChannels, dev.CalibrationTable())
	if err != nil {
		log.Fatalf("NewDecoder: %v", err)
	}

	scan, err := dev.CreateScan(ainCfg)
	if err != nil {
		log.Fatalf("CreateScan: %v", err)
	}
	if err := scan.Start(); err != nil {
		log.Fatalf("scan.Start: %v", err)
	}

	slog.Info("AIn running", "channels", len(ainChannels))

	// Stop scan when SIGINT/SIGTERM arrives; this closes scan.Chunks().
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		scan.Stop() //nolint:errcheck
	}()

	// --- Drain AIn chunks, pushing ~100 samples/sec to the viewer ---
	// Stride is rate/100 + 1 (= 1001) so consecutive pushes step through
	// the sine period, making the wave visible in the rolling window.
	const pushStride = rate/100 + 1

	v := NewViewer(2000)
	go v.Run(ctx)

	for chunk := range scan.Chunks() {
		table := decoder.DecodeVoltages(chunk.Data)
		firstInChunk := pushStride - (chunk.FrameStart % pushStride)
		if firstInChunk == pushStride {
			firstInChunk = 0
		}
		for i := firstInChunk; i < table.Frames(); i += pushStride {
			v.Push(table.Row(i)[0])
		}
	}

	if err := scan.Err(); err != nil {
		log.Fatalf("scan error: %v", err)
	}
}
