package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"
)

type dioCmd struct {
	Dir   dioDirCmd   `cmd:"" help:"Get or set pin directions."`
	Read  dioReadCmd  `cmd:"" help:"Read pin states."`
	Write dioWriteCmd `cmd:"" help:"Write to output pins."`
	Watch dioWatchCmd `cmd:"" help:"Continuously poll and display pin states."`
}

// --- dir ---

type dioDirCmd struct {
	Set    string `help:"Set directions: hex (0x0F) or IIOO notation (I=input, O=output)." default:""`
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *dioDirCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	if c.Set != "" {
		val, err := parseDIODirection(c.Set)
		if err != nil {
			return fmt.Errorf("direction: %w", err)
		}
		if err := dev.SetDigitalDirection(val); err != nil {
			return fmt.Errorf("set direction: %w", err)
		}
	}

	dir, err := dev.DigitalDirection()
	if err != nil {
		return fmt.Errorf("read direction: %w", err)
	}

	if c.Format == "json" {
		pins := make([]string, 4)
		for i := range 4 {
			if dir&(1<<(3-i)) != 0 {
				pins[i] = "input"
			} else {
				pins[i] = "output"
			}
		}
		return printJSON(map[string]any{
			"raw":  fmt.Sprintf("0x%02X", dir),
			"pins": pins,
		})
	}

	fmt.Printf("Direction: 0x%02X\n", dir)
	for i := range 4 {
		d := "output"
		if dir&(1<<i) != 0 {
			d = "input"
		}
		fmt.Printf("  Pin %d: %s\n", i, d)
	}
	return nil
}

// --- read ---

type dioReadCmd struct {
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *dioReadCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	val, err := dev.ReadDigital()
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	if c.Format == "json" {
		bits := make([]int, 4)
		for i := range 4 {
			if val&(1<<i) != 0 {
				bits[i] = 1
			}
		}
		return printJSON(map[string]any{
			"raw":  fmt.Sprintf("0x%02X", val),
			"pins": bits,
		})
	}

	fmt.Printf("Digital: 0x%02X  (", val)
	for i := 3; i >= 0; i-- {
		if val&(1<<i) != 0 {
			fmt.Print("1")
		} else {
			fmt.Print("0")
		}
	}
	fmt.Println(")")
	return nil
}

// --- write ---

type dioWriteCmd struct {
	Value string `help:"Value to write (hex, binary, or decimal)." required:""`
}

func (c *dioWriteCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	v, err := parseUintValue(c.Value)
	if err != nil {
		return fmt.Errorf("value: %w", err)
	}

	if err := dev.WriteDigital(uint8(v)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Printf("Wrote: 0x%02X\n", uint8(v))
	return nil
}

// --- watch ---

type dioWatchCmd struct {
	Interval string `help:"Polling interval." default:"100ms"`
	Format   string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *dioWatchCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	interval, err := time.ParseDuration(c.Interval)
	if err != nil {
		return fmt.Errorf("interval: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	for {
		val, err := dev.ReadDigital()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if c.Format == "json" {
			bits := make([]int, 4)
			for i := range 4 {
				if val&(1<<i) != 0 {
					bits[i] = 1
				}
			}
			if err := printJSON(map[string]any{"raw": fmt.Sprintf("0x%02X", val), "pins": bits}); err != nil {
				return err
			}
		} else {
			fmt.Printf("0x%02X  ", val)
			for i := 3; i >= 0; i-- {
				if val&(1<<i) != 0 {
					fmt.Print("1")
				} else {
					fmt.Print("0")
				}
			}
			fmt.Print("\r")
		}

		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-time.After(interval):
		}
	}
}
