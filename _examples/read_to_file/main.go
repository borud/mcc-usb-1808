package main

import (
	"log"
	"log/slog"

	"github.com/borud/mcc-usb-1808/v4/capture"
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
		Count:    200_000,
	}

	scan, err := dev.CreateScan(scanConfig)
	if err != nil {
		log.Fatal(err)
	}

	header, err := capture.NewHeader(dev, scanConfig)
	if err != nil {
		log.Fatal(err)
	}

	captureWriter, err := capture.NewWriter(
		"recordings",
		header,
		//capture.WithFileSize(1024*1024*100),
		//capture.WithBufferSize(1024*8),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer captureWriter.Close()

	scan.Start()
	for chunk := range scan.Chunks() {
		captureWriter.WriteBulk(chunk.Data)
	}

	if scan.Err() != nil {
		log.Fatal(scan.Err())
	}

	slog.Info("frames written", "num", captureWriter.FramesWritten())
}
