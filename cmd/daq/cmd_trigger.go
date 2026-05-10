package main

import (
	"fmt"
	"os"

	"github.com/borud/mcc-usb-1808"
)

type triggerCmd struct {
	Show triggerShowCmd `cmd:"" help:"Display current trigger configuration."`
	Set  triggerSetCmd  `cmd:"" help:"Configure external trigger."`
}

// --- show ---

type triggerShowCmd struct{}

func (c *triggerShowCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	cfg, err := dev.TriggerConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: trigger config register is write-only on this firmware\n")
		return nil
	}

	mode := "level"
	if cfg&usb1808.TriggerEdge != 0 {
		mode = "edge"
	}
	polarity := "low/falling"
	if cfg&usb1808.TriggerHigh != 0 {
		polarity = "high/rising"
	}

	if app.Format == "json" {
		return printJSON(map[string]any{
			"raw":      fmt.Sprintf("0x%02X", cfg),
			"mode":     mode,
			"polarity": polarity,
		})
	}

	fmt.Printf("Trigger: 0x%02X\n", cfg)
	fmt.Printf("  Mode:     %s\n", mode)
	fmt.Printf("  Polarity: %s\n", polarity)
	return nil
}

// --- set ---

type triggerSetCmd struct {
	Mode     string `help:"Trigger mode (${enum})." default:"edge" enum:"level,edge"`
	Polarity string `help:"Trigger polarity (${enum})." default:"rising" enum:"low,high,falling,rising"`
}

func (c *triggerSetCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	var cfg uint8
	if c.Mode == "edge" {
		cfg |= usb1808.TriggerEdge
	}
	switch c.Polarity {
	case "high", "rising":
		cfg |= usb1808.TriggerHigh
	}

	if err := dev.SetTriggerConfig(cfg); err != nil {
		return fmt.Errorf("set: %w", err)
	}
	fmt.Printf("Trigger set: %s, %s (0x%02X)\n", c.Mode, c.Polarity, cfg)
	return nil
}
