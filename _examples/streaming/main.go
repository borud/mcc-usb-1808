package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/borud/mcc-usb-1808/v4/capture"
	"github.com/borud/mcc-usb-1808/v4/codec"
	"github.com/borud/mcc-usb-1808/v4/device"
	"github.com/borud/mcc-usb-1808/v4/stream"
)

func main() {
	dev, err := device.Open()
	if err != nil {
		log.Fatal(err)
	}

	channelConfig := []device.ChannelConfig{
		{Index: 0, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 1, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 2, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 3, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.Differential},
		{Index: 4, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 5, Type: device.ChannelTypeAnalog, Range: device.BP10V, Mode: device.SingleEnded},
		{Index: 6, Type: device.ChannelTypeAnalog, Range: device.BP5V, Mode: device.SingleEnded},
		{Index: 7, Type: device.ChannelTypeAnalog, Range: device.BP5V, Mode: device.SingleEnded},
	}

	scanConfig := device.ScanConfig{
		Channels: channelConfig,
		Rate:     200_000,
		Count:    1000_000,
	}

	decoder, err := codec.NewDecoder(channelConfig, dev.CalibrationTable())
	if err != nil {
		log.Fatal(err)
	}

	scan, err := dev.CreateScan(scanConfig)
	if err != nil {
		log.Fatal(err)
	}

	header, err := capture.NewHeader(dev, scanConfig)
	if err != nil {
		log.Fatal(err)
	}

	fanout := stream.NewFanout(scan.Chunks())
	disk := fanout.Subscribe(stream.WithDepth(64))
	live := fanout.Subscribe(stream.WithDepth(8))
	go fanout.Run()

	tmpDir, err := os.MkdirTemp("", "stream-capture-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Disk writer goroutine.
	go func() {
		cw, err := capture.NewWriter(filepath.Join(tmpDir, "data"), header)
		if err != nil {
			log.Fatal(err)
		}
		defer cw.Close()

		for chunk := range disk.Chunks() {
			if err := cw.WriteBulk(chunk.Data); err != nil {
				log.Fatal(err)
			}
			disk.Release(chunk.Data)
		}

		slog.Info("frames written", "num", cw.FramesWritten())
	}()

	if err := scan.Start(); err != nil {
		log.Fatal(err)
	}

	// Live display: bulk-decode each chunk and print per-frame voltages.
	var totalFrames int64
	var totalChunks int64
	for chunk := range live.Chunks() {
		table := decoder.DecodeVoltages(chunk.Data)
		for i := range table.Frames() {
			row := table.Row(i)
			fmt.Printf("ch0=%.4f ch1=%.4f ch2=%.4f ch3=%.4f ch4=%.4f ch5=%.4f ch6=%.4f ch7=%.4f\n",
				row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7])
		}
		totalFrames += int64(table.Frames())
		totalChunks++
		live.Release(chunk.Data)
	}

	if scan.Err() != nil {
		log.Fatal(scan.Err())
	}

	fanout.AwaitDone()

	liveReceived := totalChunks
	liveDropped := live.Dropped()
	diskDropped := disk.Dropped()
	sourceChunks := liveReceived + int64(liveDropped)

	slog.Info("source", "chunks", sourceChunks)
	slog.Info("disk", "dropped", diskDropped)
	slog.Info("live", "received", liveReceived, "dropped", liveDropped, "frames", totalFrames)

}
