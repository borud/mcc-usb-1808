package main

import "fmt"

type blinkCmd struct {
	Count uint8 `help:"Number of blinks." default:"5"`
}

func (c *blinkCmd) Run(app *cli) error {
	dev, err := openDevice(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	fmt.Printf("Blinking LED %d times on %s...\n", c.Count, dev.Model())
	return dev.BlinkLED(c.Count)
}
