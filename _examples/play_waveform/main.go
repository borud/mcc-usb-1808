// play_waveform demonstrates PlayOnce (single-shot) and Loop (continuous)
// analog output. It generates a 1 kHz sine wave table and plays it on DAC0.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/borud/mcc-usb-1808/v4/device"
)

const (
	rate        = 100_000 // Hz
	sineHz      = 1_000   // Hz
	amplitude   = 5.0     // Volts peak
	loopSeconds = 3
)

func main() {
	dev, err := device.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()

	if err := dev.Init(); err != nil {
		log.Fatal(err)
	}

	// Build one period of a 1 kHz sine wave at 100 kS/s.
	samplesPerPeriod := rate / sineHz
	period := make([]float64, samplesPerPeriod)
	for i := range samplesPerPeriod {
		period[i] = amplitude * math.Sin(2*math.Pi*float64(i)/float64(samplesPerPeriod))
	}

	channels := []int{0}

	// --- PlayOnce: output one period and wait for completion ---
	fmt.Println("PlayOnce: outputting one sine period on DAC0")
	h, err := dev.PlayOnce(channels, rate, period)
	if err != nil {
		log.Fatalf("PlayOnce: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Wait(ctx); err != nil {
		log.Fatalf("PlayOnce.Wait: %v", err)
	}
	fmt.Println("PlayOnce done")

	time.Sleep(200 * time.Millisecond)

	// --- Loop: play continuously for 3 seconds then stop ---
	fmt.Printf("Loop: playing %d Hz sine for %d s on DAC0\n", sineHz, loopSeconds)
	lh, err := dev.Loop(channels, rate, period)
	if err != nil {
		log.Fatalf("Loop: %v", err)
	}
	time.Sleep(loopSeconds * time.Second)
	if err := lh.Stop(); err != nil {
		log.Fatalf("Loop.Stop: %v", err)
	}
	fmt.Println("Loop stopped")
}
