package main

import (
	"fmt"

	"github.com/borud/mcc-usb-1808"
)

type calCmd struct {
	Date  calDateCmd  `cmd:"" help:"Show factory calibration date."`
	Table calTableCmd `cmd:"" help:"Show calibration coefficient table."`
}

// --- date ---

type calDateCmd struct{}

func (c *calDateCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	calDate, err := dev.CalibrationDate()
	if err != nil {
		return fmt.Errorf("cal date: %w", err)
	}

	if app.Format == "json" {
		return printJSON(map[string]string{
			"cal_date": calDate.Format("2006-01-02T15:04:05Z"),
		})
	}

	fmt.Printf("Calibration date: %s\n", calDate.Format("2006-01-02 15:04:05"))
	return nil
}

// --- table ---

type calTableCmd struct {
	Channel int    `help:"Filter to specific channel (-1 for all)." default:"-1"`
	Output  string `help:"Which table (${enum})." default:"ain" enum:"ain,aout"`
}

func (c *calTableCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	if c.Output == "aout" {
		return c.showAOut(app, dev)
	}
	return c.showAIn(app, dev)
}

func (c *calTableCmd) showAIn(app *cli, dev *usb1808.Device) error {
	table := dev.AnalogInCalTable()
	rangeLabels := []string{"±10V", "±5V", "0-10V", "0-5V"}

	if app.Format == "json" {
		var entries []map[string]any
		for ch := range 8 {
			if c.Channel >= 0 && ch != c.Channel {
				continue
			}
			for r := range 4 {
				entries = append(entries, map[string]any{
					"channel": ch,
					"range":   rangeLabels[r],
					"slope":   table[ch][r].Slope,
					"offset":  table[ch][r].Offset,
				})
			}
		}
		return printJSON(map[string]any{"ain_calibration": entries})
	}

	if app.Format == "csv" {
		fmt.Println("channel,range,slope,offset")
		for ch := range 8 {
			if c.Channel >= 0 && ch != c.Channel {
				continue
			}
			for r := range 4 {
				fmt.Printf("%d,%s,%g,%g\n", ch, rangeLabels[r], table[ch][r].Slope, table[ch][r].Offset)
			}
		}
		return nil
	}

	fmt.Println("Analog Input Calibration")
	fmt.Printf("  %-4s  %-6s  %12s  %12s\n", "CH", "Range", "Slope", "Offset")
	for ch := range 8 {
		if c.Channel >= 0 && ch != c.Channel {
			continue
		}
		for r := range 4 {
			fmt.Printf("  %-4d  %-6s  %12.6f  %12.6f\n", ch, rangeLabels[r], table[ch][r].Slope, table[ch][r].Offset)
		}
	}
	return nil
}

func (c *calTableCmd) showAOut(app *cli, dev *usb1808.Device) error {
	table := dev.AnalogOutCalTable()

	if app.Format == "json" {
		var entries []map[string]any
		for ch := range 2 {
			if c.Channel >= 0 && ch != c.Channel {
				continue
			}
			entries = append(entries, map[string]any{
				"channel": ch,
				"slope":   table[ch].Slope,
				"offset":  table[ch].Offset,
			})
		}
		return printJSON(map[string]any{"aout_calibration": entries})
	}

	if app.Format == "csv" {
		fmt.Println("channel,slope,offset")
		for ch := range 2 {
			if c.Channel >= 0 && ch != c.Channel {
				continue
			}
			fmt.Printf("%d,%g,%g\n", ch, table[ch].Slope, table[ch].Offset)
		}
		return nil
	}

	fmt.Println("Analog Output Calibration")
	fmt.Printf("  %-4s  %12s  %12s\n", "CH", "Slope", "Offset")
	for ch := range 2 {
		if c.Channel >= 0 && ch != c.Channel {
			continue
		}
		fmt.Printf("  %-4d  %12.6f  %12.6f\n", ch, table[ch].Slope, table[ch].Offset)
	}
	return nil
}
