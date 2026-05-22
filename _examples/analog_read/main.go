// Example analog_read demonstrates a finite scan of all 8 analog input
// channels at 200 kS/s.
//
// The scan loop must keep up with the sample rate.  At 200 kS/s × 8
// channels the device produces 6.4 MB/s.  If the host doesn't drain
// Chunks() fast enough, the on-device FIFO overruns and data is lost.
// To avoid this we decode into a pre-allocated buffer and collect all
// voltages first, then print after the scan is complete.
package main

import (
	"fmt"
	"log"

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

	// Collect all voltages first.  The scan loop must be fast — any
	// per-frame I/O (like fmt.Printf) creates backpressure that stalls
	// the USB readers, which causes the device FIFO to overrun.
	nCh := len(channelConfig)
	buf := make([]float64, decoder.MaxSamples())
	voltages := make([]float64, 0, int(scanConfig.Count)*nCh)

	for chunk := range scan.Chunks() {
		nFrames := decoder.DecodeVoltages(chunk.Data, buf)
		voltages = append(voltages, buf[:nFrames*nCh]...)
	}

	if err := scan.Stop(); err != nil {
		log.Fatal(err)
	}

	// Print after the scan is done — safe to be slow here.
	totalFrames := len(voltages) / nCh
	for i := range totalFrames {
		fmt.Printf("%10d", i+1)
		for ch := range nCh {
			fmt.Printf(" | %012.8f", voltages[i*nCh+ch])
		}
		fmt.Println()
	}

	fmt.Printf("\n%d frames captured\n", totalFrames)
}
