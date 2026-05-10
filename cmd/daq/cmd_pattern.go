package main

import (
	"fmt"
	"os"

	"github.com/borud/mcc-usb-1808"
)

type patternCmd struct {
	Show patternShowCmd `cmd:"" help:"Display current pattern detection configuration."`
	Set  patternSetCmd  `cmd:"" help:"Configure pattern detection."`
}

// --- show ---

type patternShowCmd struct {
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *patternShowCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	cfg, err := dev.PatternDetect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "note: pattern detection register is write-only on this firmware\n")
		return nil
	}

	cmpMode := compareModeName(cfg.Options)

	if c.Format == "json" {
		return printJSON(map[string]any{
			"value":   fmt.Sprintf("0x%02X", cfg.Value),
			"mask":    fmt.Sprintf("0x%02X", cfg.Mask),
			"compare": cmpMode,
			"options": fmt.Sprintf("0x%02X", cfg.Options),
		})
	}

	fmt.Printf("Pattern Detection\n")
	fmt.Printf("  Value:   0x%02X\n", cfg.Value)
	fmt.Printf("  Mask:    0x%02X\n", cfg.Mask)
	fmt.Printf("  Compare: %s (0x%02X)\n", cmpMode, cfg.Options)
	return nil
}

// --- set ---

type patternSetCmd struct {
	Value   string `help:"Pattern value (hex)." required:""`
	Mask    string `help:"Comparison mask (hex)." default:"0x0F"`
	Compare string `help:"Comparison mode (${enum})." default:"equal" enum:"equal,not-equal,greater,less"`
}

func (c *patternSetCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	val, err := parseUintValue(c.Value)
	if err != nil {
		return fmt.Errorf("value: %w", err)
	}
	mask, err := parseUintValue(c.Mask)
	if err != nil {
		return fmt.Errorf("mask: %w", err)
	}

	var opts uint8
	switch c.Compare {
	case "equal":
		opts = usb1808.PatternEqual
	case "not-equal":
		opts = usb1808.PatternNotEqual
	case "greater":
		opts = usb1808.PatternGreaterThn
	case "less":
		opts = usb1808.PatternLessThan
	}

	cfg := usb1808.PatternDetectConfig{
		Value:   uint8(val),
		Mask:    uint8(mask),
		Options: opts,
	}
	if err := dev.SetPatternDetect(cfg); err != nil {
		return fmt.Errorf("set: %w", err)
	}
	fmt.Printf("Pattern set: value=0x%02X mask=0x%02X compare=%s\n", cfg.Value, cfg.Mask, c.Compare)
	return nil
}

func compareModeName(opts uint8) string {
	switch opts & 0x06 {
	case usb1808.PatternEqual:
		return "equal"
	case usb1808.PatternNotEqual:
		return "not-equal"
	case usb1808.PatternGreaterThn:
		return "greater"
	case usb1808.PatternLessThan:
		return "less"
	default:
		return "unknown"
	}
}
