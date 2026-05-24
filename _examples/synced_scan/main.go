// synced_scan demonstrates simultaneous analog input and output using a shared
// software trigger. DAC0 outputs a 1 kHz sine wave while AIn channels 0–3
// record the response at 100 kS/s for 1 second.
//
// For hardware-synchronized operation (minimal phase skew), replace the
// software trigger with an external pulse connected to the TRIG_IN pin,
// configure both scans with ScanOptExternalTrigger, and call SetTriggerConfig
// before starting either scan. See docs/sync.md for details.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"math"
	"time"

	"github.com/borud/mcc-usb-1808/v4/device"
)

const (
	rate           = 100_000 // Hz
	sineHz         = 1_000   // Hz
	amplitude      = 5.0     // Volts peak
	recordSeconds  = 1
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

	// --- Configure AIn: record 4 channels for recordSeconds ---
	ainChannels := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 2, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 3, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
	}
	ainCfg := device.ScanConfig{
		Channels: ainChannels,
		Rate:     rate,
		Count:    uint32(rate * recordSeconds),
	}

	scan, err := dev.CreateScan(ainCfg)
	if err != nil {
		log.Fatalf("CreateScan: %v", err)
	}
	if err := scan.Start(); err != nil {
		log.Fatalf("scan.Start: %v", err)
	}

	slog.Info("AIn running", "channels", len(ainChannels), "duration_s", recordSeconds)

	// --- Drain AIn chunks ---
	totalChunks := 0
	totalBytes := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(recordSeconds+5)*time.Second)
	defer cancel()

	for chunk := range scan.Chunks() {
		totalChunks++
		totalBytes += len(chunk.Data)

		select {
		case <-ctx.Done():
			log.Fatal("timed out waiting for scan to complete")
		default:
		}
	}

	if err := scan.Err(); err != nil {
		log.Fatalf("scan error: %v", err)
	}

	fmt.Printf("AI complete: %d chunks, %d bytes total\n", totalChunks, totalBytes)
	slog.Info("synced_scan complete", "chunks", totalChunks, "bytes", totalBytes)
}
