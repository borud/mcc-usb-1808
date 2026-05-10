package main

import (
	"fmt"
	"time"

	"github.com/borud/mcc-usb-1808"
)

type analogReadCmd struct {
	Channels string `help:"Channels to read (e.g. 0-7 or 0,2,4)." default:"0-7"`
	Range    string `help:"Voltage range (bp10v,bp5v,up10v,up5v). Single or per-channel." default:"bp10v"`
	Mode     string `help:"Input mode (${enum})." default:"differential" enum:"differential,single-ended,grounded"`
	Raw      bool   `help:"Output raw 18-bit ADC values." default:"false"`
	Repeat   int    `help:"Number of reads." default:"1"`
	Interval string `help:"Delay between repeats." default:"1s"`
	Format   string `help:"Output format (${enum})." default:"text" enum:"text,csv,json"`
}

func (c *analogReadCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	channels, err := parseChannels(c.Channels)
	if err != nil {
		return fmt.Errorf("channels: %w", err)
	}

	ranges, err := parseRanges(c.Range, len(channels))
	if err != nil {
		return fmt.Errorf("range: %w", err)
	}

	mode, err := parseMode(c.Mode)
	if err != nil {
		return err
	}

	if err := configureAnalogInputs(dev, channels, ranges, mode); err != nil {
		return fmt.Errorf("configure: %w", err)
	}

	interval, err := time.ParseDuration(c.Interval)
	if err != nil {
		return fmt.Errorf("interval: %w", err)
	}

	for i := range c.Repeat {
		if i > 0 {
			time.Sleep(interval)
		}

		if c.Raw {
			raw, err := dev.AnalogInRaw()
			if err != nil {
				return fmt.Errorf("read: %w", err)
			}
			if err := c.outputRaw(channels, raw); err != nil {
				return err
			}
		} else {
			volts, err := dev.AnalogIn()
			if err != nil {
				return fmt.Errorf("read: %w", err)
			}
			if err := c.outputVolts(channels, ranges, volts); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *analogReadCmd) outputVolts(channels []int, ranges []usb1808.Range, volts [usb1808.NumAInChannels]float64) error {
	switch c.Format {
	case "json":
		entries := make([]map[string]any, len(channels))
		for i, ch := range channels {
			entries[i] = map[string]any{
				"channel": ch,
				"range":   ranges[i].String(),
				"voltage": volts[ch],
			}
		}
		return printJSON(map[string]any{"channels": entries})

	case "csv":
		// Header.
		for i, ch := range channels {
			if i > 0 {
				fmt.Print(",")
			}
			fmt.Printf("ch%d", ch)
		}
		fmt.Println()
		// Data.
		for i, ch := range channels {
			if i > 0 {
				fmt.Print(",")
			}
			fmt.Printf("%.6f", volts[ch])
		}
		fmt.Println()

	default:
		for i, ch := range channels {
			fmt.Printf("  CH%d (%s): %+9.4f V\n", ch, ranges[i], volts[ch])
		}
	}
	return nil
}

func (c *analogReadCmd) outputRaw(channels []int, raw [usb1808.NumAInChannels]uint32) error {
	switch c.Format {
	case "json":
		entries := make([]map[string]any, len(channels))
		for i, ch := range channels {
			entries[i] = map[string]any{
				"channel": ch,
				"raw":     raw[ch],
			}
		}
		return printJSON(map[string]any{"channels": entries})

	case "csv":
		for i, ch := range channels {
			if i > 0 {
				fmt.Print(",")
			}
			fmt.Printf("ch%d", ch)
		}
		fmt.Println()
		for i, ch := range channels {
			if i > 0 {
				fmt.Print(",")
			}
			fmt.Printf("%d", raw[ch])
		}
		fmt.Println()

	default:
		for _, ch := range channels {
			fmt.Printf("  CH%d: %d (0x%05X)\n", ch, raw[ch], raw[ch])
		}
	}
	return nil
}
