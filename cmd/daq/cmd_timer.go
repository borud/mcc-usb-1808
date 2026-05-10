package main

import (
	"fmt"

	"github.com/borud/mcc-usb-1808"
)

type timerCmd struct {
	Start  timerStartCmd  `cmd:"" help:"Configure and start a timer."`
	Stop   timerStopCmd   `cmd:"" help:"Stop a timer."`
	Status timerStatusCmd `cmd:"" help:"Show timer state."`
}

// --- start ---

type timerStartCmd struct {
	Index     int     `help:"Timer index (0 or 1)." default:"0"`
	Frequency float64 `help:"Output frequency in Hz." required:""`
	DutyCycle float64 `help:"Duty cycle (0.0-1.0)." default:"0.5" name:"duty-cycle"`
	Count     uint32  `help:"Number of pulses (0=continuous)." default:"0"`
	Delay     float64 `help:"Initial delay in seconds." default:"0"`
	Inverted  bool    `help:"Invert output polarity." default:"false"`
}

func (c *timerStartCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	cfg := usb1808.TimerConfig{
		Frequency: c.Frequency,
		DutyCycle: c.DutyCycle,
		Count:     c.Count,
		Delay:     c.Delay,
	}

	if err := dev.SetTimerParams(c.Index, cfg); err != nil {
		return fmt.Errorf("set params: %w", err)
	}

	control := uint8(usb1808.TimerEnable)
	if c.Inverted {
		control |= usb1808.TimerInverted
	}
	if err := dev.SetTimerControl(c.Index, control); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	fmt.Printf("Timer %d started: %.2f Hz, %.1f%% duty cycle", c.Index, c.Frequency, c.DutyCycle*100)
	if c.Count > 0 {
		fmt.Printf(", %d pulses", c.Count)
	}
	if c.Delay > 0 {
		fmt.Printf(", %.3fs delay", c.Delay)
	}
	if c.Inverted {
		fmt.Print(", inverted")
	}
	fmt.Println()
	return nil
}

// --- stop ---

type timerStopCmd struct {
	Index int `help:"Timer index (0 or 1)." default:"0"`
}

func (c *timerStopCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	if err := dev.StopTimer(c.Index); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	fmt.Printf("Timer %d stopped.\n", c.Index)
	return nil
}

// --- status ---

type timerStatusCmd struct {
	Index int `help:"Timer index (0 or 1)." default:"0"`
}

func (c *timerStatusCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	ctrl, err := dev.TimerControl(c.Index)
	if err != nil {
		return fmt.Errorf("control: %w", err)
	}

	params, err := dev.TimerParams(c.Index)
	if err != nil {
		return fmt.Errorf("params: %w", err)
	}

	if app.Format == "json" {
		return printJSON(map[string]any{
			"index":      c.Index,
			"enabled":    ctrl&usb1808.TimerEnable != 0,
			"running":    ctrl&usb1808.TimerRunning != 0,
			"inverted":   ctrl&usb1808.TimerInverted != 0,
			"frequency":  params.Frequency,
			"duty_cycle": params.DutyCycle,
			"count":      params.Count,
			"delay":      params.Delay,
			"control":    fmt.Sprintf("0x%02X", ctrl),
		})
	}

	fmt.Printf("Timer %d\n", c.Index)
	fmt.Printf("  Enabled:    %v\n", ctrl&usb1808.TimerEnable != 0)
	fmt.Printf("  Running:    %v\n", ctrl&usb1808.TimerRunning != 0)
	fmt.Printf("  Inverted:   %v\n", ctrl&usb1808.TimerInverted != 0)
	fmt.Printf("  Control:    0x%02X\n", ctrl)
	fmt.Printf("  Frequency:  %.2f Hz\n", params.Frequency)
	fmt.Printf("  Duty cycle: %.1f%%\n", params.DutyCycle*100)
	fmt.Printf("  Count:      %d\n", params.Count)
	fmt.Printf("  Delay:      %.6f s\n", params.Delay)
	return nil
}
