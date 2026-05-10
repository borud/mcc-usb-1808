package main

import "fmt"

type analogOutCmd struct {
	Channel int     `help:"Output channel (0 or 1)." default:"0"`
	Voltage float64 `help:"Voltage to output (±10V)." required:""`
	Raw     bool    `help:"Write raw 16-bit DAC value instead." default:"false"`
}

func (c *analogOutCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	if c.Raw {
		if err := dev.AnalogOutRaw(c.Channel, uint16(c.Voltage)); err != nil {
			return fmt.Errorf("analog out: %w", err)
		}
		fmt.Printf("CH%d: raw %d\n", c.Channel, uint16(c.Voltage))
	} else {
		if err := dev.AnalogOut(c.Channel, c.Voltage); err != nil {
			return fmt.Errorf("analog out: %w", err)
		}
		fmt.Printf("CH%d: %.4f V\n", c.Channel, c.Voltage)
	}
	return nil
}
