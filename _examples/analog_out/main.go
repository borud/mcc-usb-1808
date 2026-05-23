// analog_out demonstrates single-shot DAC output by ramping from −10 V to
// +10 V in 1 V steps on both channels with 200 ms between steps.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/borud/mcc-usb-1808/v4/device"
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

	fmt.Println("ramping DAC0 and DAC1 from -10 V to +10 V")
	for v := -10.0; v <= 10.0; v++ {
		for ch := range device.NumAOutChannels {
			if err := dev.AnalogOut(ch, v); err != nil {
				log.Fatalf("AnalogOut(ch=%d, %.1f V): %v", ch, v, err)
			}
		}
		fmt.Printf("  %.1f V\n", v)
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Println("done — device will zero DACs on close")
}
