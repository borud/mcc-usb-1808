package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"
)

type counterCmd struct {
	Read   counterReadCmd   `cmd:"" help:"Read counter or encoder value."`
	Write  counterWriteCmd  `cmd:"" help:"Set counter value."`
	Config counterConfigCmd `cmd:"" help:"Configure counter mode and options."`
	Watch  counterWatchCmd  `cmd:"" help:"Continuously poll and display counter value."`
}

// --- read ---

type counterReadCmd struct {
	Index  int    `help:"Counter/encoder index (0-3)." default:"0"`
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *counterReadCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	val, err := dev.ReadCounter(c.Index)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	name := counterName(c.Index)
	if c.Format == "json" {
		return printJSON(map[string]any{"index": c.Index, "name": name, "value": val})
	}
	fmt.Printf("%s: %d\n", name, val)
	return nil
}

// --- write ---

type counterWriteCmd struct {
	Index int    `help:"Counter/encoder index (0-3)." default:"0"`
	Value uint32 `help:"Value to write." default:"0"`
}

func (c *counterWriteCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	if err := dev.WriteCounter(c.Index, c.Value); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Printf("%s: set to %d\n", counterName(c.Index), c.Value)
	return nil
}

// --- config ---

type counterConfigCmd struct {
	Index      int    `help:"Counter/encoder index (0-3)." default:"0"`
	Mode       string `help:"Counter mode: totalize, period, pulse-width, timing." default:""`
	Options    string `help:"Counter options (comma-separated): clear-on-read,no-recycle,count-down,range-limit,falling-edge." default:""`
	EncMode    string `help:"Encoder mode: x1, x2, x4." default:"" name:"encoder-mode"`
	EncOptions string `help:"Encoder options (comma-separated): clear-on-z,latch-on-z,no-recycle,range-limit." default:"" name:"encoder-options"`
	PeriodMult string `help:"Period multiplier: 1x, 10x, 100x, 1000x." default:"" name:"period-mult"`
	TickSize   string `help:"Tick size: 20ns, 200ns, 2us, 20us." default:"" name:"tick-size"`
	Min        *int64 `help:"Minimum limit value." name:"min"`
	Max        *int64 `help:"Maximum limit value." name:"max"`
	Show       bool   `help:"Show current config without changing." default:"false"`
	Format     string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *counterConfigCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	isEncoder := c.Index >= 2

	if !c.Show {
		if isEncoder && c.EncMode != "" {
			opts, err := encoderOptionsByte(c.EncMode, c.EncOptions)
			if err != nil {
				return err
			}
			if err := dev.SetCounterOptions(c.Index, opts); err != nil {
				return fmt.Errorf("set encoder options: %w", err)
			}
		}

		if !isEncoder && c.Mode != "" {
			mode, err := counterModeByte(c.Mode, c.PeriodMult, c.TickSize)
			if err != nil {
				return err
			}
			if err := dev.SetCounterMode(c.Index, mode); err != nil {
				return fmt.Errorf("set mode: %w", err)
			}
		}

		if !isEncoder && c.Options != "" {
			opts, err := counterOptionsByte(c.Options)
			if err != nil {
				return err
			}
			if err := dev.SetCounterOptions(c.Index, opts); err != nil {
				return fmt.Errorf("set options: %w", err)
			}
		}

		if c.Min != nil {
			if err := dev.SetCounterLimits(c.Index, 0, uint32(*c.Min)); err != nil {
				return fmt.Errorf("set min limit: %w", err)
			}
		}
		if c.Max != nil {
			if err := dev.SetCounterLimits(c.Index, 1, uint32(*c.Max)); err != nil {
				return fmt.Errorf("set max limit: %w", err)
			}
		}
	}

	// Show current config.
	name := counterName(c.Index)
	val, _ := dev.ReadCounter(c.Index)
	opts, _ := dev.CounterOptions(c.Index)

	if c.Format == "json" {
		info := map[string]any{
			"index":   c.Index,
			"name":    name,
			"value":   val,
			"options": fmt.Sprintf("0x%02X", opts),
		}
		if !isEncoder {
			mode, _ := dev.CounterMode(c.Index)
			info["mode"] = fmt.Sprintf("0x%02X", mode)
		}
		minVal, _ := dev.CounterLimits(c.Index, 0)
		maxVal, _ := dev.CounterLimits(c.Index, 1)
		info["min_limit"] = minVal
		info["max_limit"] = maxVal
		return printJSON(info)
	}

	fmt.Printf("%s (index %d)\n", name, c.Index)
	fmt.Printf("  Value:   %d\n", val)
	fmt.Printf("  Options: 0x%02X\n", opts)
	if !isEncoder {
		mode, _ := dev.CounterMode(c.Index)
		fmt.Printf("  Mode:    0x%02X\n", mode)
	}
	minVal, _ := dev.CounterLimits(c.Index, 0)
	maxVal, _ := dev.CounterLimits(c.Index, 1)
	fmt.Printf("  Min:     %d\n", minVal)
	fmt.Printf("  Max:     %d\n", maxVal)
	return nil
}

// --- watch ---

type counterWatchCmd struct {
	Index    int    `help:"Counter/encoder index (0-3)." default:"0"`
	Interval string `help:"Polling interval." default:"100ms"`
	Format   string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *counterWatchCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	interval, err := time.ParseDuration(c.Interval)
	if err != nil {
		return fmt.Errorf("interval: %w", err)
	}

	name := counterName(c.Index)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	for {
		val, err := dev.ReadCounter(c.Index)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if c.Format == "json" {
			if err := printJSON(map[string]any{"index": c.Index, "name": name, "value": val}); err != nil {
				return err
			}
		} else {
			fmt.Printf("%s: %d\r", name, val)
		}

		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-time.After(interval):
		}
	}
}

func counterName(index int) string {
	switch index {
	case 0:
		return "Counter0"
	case 1:
		return "Counter1"
	case 2:
		return "Encoder0"
	case 3:
		return "Encoder1"
	default:
		return fmt.Sprintf("Counter%d", index)
	}
}
