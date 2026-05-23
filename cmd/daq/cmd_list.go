package main

import (
	"fmt"

	"github.com/borud/mcc-usb-1808/v4/device"
)

type listCmd struct{}

func (c *listCmd) Run(_ *cli) error {
	devices, err := device.Discover()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("no devices found")
		return nil
	}
	fmt.Printf("%-12s  %-4s  %-4s\n", "Model", "Bus", "Addr")
	for _, d := range devices {
		fmt.Printf("%-12s  %-4d  %-4d\n", d.Model, d.Bus, d.Address)
	}
	return nil
}
