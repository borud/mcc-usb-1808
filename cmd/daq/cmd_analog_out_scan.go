package main

import (
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/borud/mcc-usb-1808"
)

type analogOutScanCmd struct {
	Channels string  `help:"Output channels (e.g. 0 or 0,1)." default:"0"`
	Rate     float64 `help:"Output sample rate in Hz." default:"10000"`
	Count    int     `help:"Number of scans (0=continuous)." default:"0"`
	Input    string  `help:"Input CSV file with voltage data." short:"i" default:"-"`
	Trigger  string  `help:"Trigger mode (${enum})." default:"none" enum:"none,rising,falling,high,low"`
	Loop     bool    `help:"Loop the input data." default:"false"`
}

func (c *analogOutScanCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	channels, err := parseChannels(c.Channels)
	if err != nil {
		return fmt.Errorf("channels: %w", err)
	}
	for _, ch := range channels {
		if ch < 0 || ch > 2 {
			return fmt.Errorf("invalid output channel: %d (valid: 0, 1, or 2 for DIO)", ch)
		}
	}

	// Read input data.
	var in *os.File
	if c.Input == "-" {
		in = os.Stdin
	} else {
		f, err := os.Open(c.Input)
		if err != nil {
			return fmt.Errorf("open input: %w", err)
		}
		defer f.Close()
		in = f
	}

	reader := csv.NewReader(in)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("read CSV: %w", err)
	}

	// Skip header row if first cell is not numeric.
	startRow := 0
	if len(records) > 0 && len(records[0]) > 0 {
		if _, parseErr := strconv.ParseFloat(strings.TrimSpace(records[0][0]), 64); parseErr != nil {
			startRow = 1
		}
	}

	nCh := len(channels)
	nScans := len(records) - startRow
	if nScans <= 0 {
		return fmt.Errorf("no data rows in input")
	}

	// Convert voltages to 16-bit LE DAC values.
	data := make([]byte, nScans*nCh*2)
	for i := startRow; i < len(records); i++ {
		if len(records[i]) < nCh {
			return fmt.Errorf("row %d: expected %d columns, got %d", i+1, nCh, len(records[i]))
		}
		for j := 0; j < nCh; j++ {
			v, parseErr := strconv.ParseFloat(strings.TrimSpace(records[i][j]), 64)
			if parseErr != nil {
				return fmt.Errorf("row %d col %d: %w", i+1, j+1, parseErr)
			}
			var dac uint16
			if channels[j] < usb1808.NumAOutChannels {
				dac = dev.VoltsToAnalogOut(v, channels[j])
			} else {
				dac = uint16(v)
			}
			offset := ((i - startRow) * nCh + j) * 2
			binary.LittleEndian.PutUint16(data[offset:], dac)
		}
	}

	// Configure output scan queue.
	if err := dev.ConfigureAnalogOutScan(channels); err != nil {
		return fmt.Errorf("configure: %w", err)
	}

	// Trigger setup.
	var opts uint8
	if c.Trigger != "none" {
		opts |= usb1808.AOutOptTrigger
		if err := dev.SetTriggerConfig(triggerConfigByte(c.Trigger)); err != nil {
			return fmt.Errorf("trigger: %w", err)
		}
	}

	scanCount := uint32(c.Count)
	if scanCount == 0 && !c.Loop {
		scanCount = uint32(nScans)
	}

	cfg := usb1808.AnalogOutScanConfig{
		Channels: channels,
		Rate:     c.Rate,
		Count:    scanCount,
		Options:  opts,
	}

	if err := dev.StartAnalogOutScan(cfg); err != nil {
		return fmt.Errorf("start scan: %w", err)
	}
	defer dev.StopAnalogOutScan()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if c.Loop {
		for {
			if ctx.Err() != nil {
				break
			}
			if _, err := dev.WriteAnalogOutScan(data); err != nil {
				return fmt.Errorf("write: %w", err)
			}
		}
	} else {
		if _, err := dev.WriteAnalogOutScan(data); err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}

	fmt.Printf("Output scan complete: %d scans on %d channel(s) at %.0f Hz\n", nScans, nCh, c.Rate)
	return nil
}
