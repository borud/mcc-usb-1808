package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/borud/mcc-usb-1808"
)

type analogScanCmd struct {
	Channels  string  `help:"Analog input channels (e.g. 0-3 or 0,2,4)." default:"0-3"`
	Queue     string  `help:"Mixed scan queue (e.g. ain0,ain1,dio,counter0)." default:""`
	Range     string  `help:"Voltage range (bp10v,bp5v,up10v,up5v)." default:"bp10v"`
	Mode      string  `help:"Input mode (${enum})." default:"differential" enum:"differential,single-ended,grounded"`
	Rate      float64 `help:"Sample rate in Hz per channel." default:"10000"`
	Count     int     `help:"Number of scans (0=continuous)." default:"100"`
	Trigger   string  `help:"Trigger mode (${enum})." default:"none" enum:"none,rising,falling,high,low"`
	Retrigger uint32  `help:"Scans per trigger event (0=disabled)." default:"0"`
	Output    string        `help:"Output file." short:"o" default:"-"`
	Timestamp string        `help:"Timestamp format (${enum})." default:"elapsed" enum:"elapsed,unix,iso8601,none"`
	Format    string        `help:"Output format (${enum})." default:"text" enum:"text,json"`
	Flush     time.Duration `help:"Flush output interval (0=fully buffered)." default:"0"`
}

func (c *analogScanCmd) Run(app *cli) error {
	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	// Build scan queue.
	var queue []int
	if c.Queue != "" {
		queue, err = parseQueue(c.Queue)
		if err != nil {
			return fmt.Errorf("queue: %w", err)
		}
	} else {
		queue, err = parseChannels(c.Channels)
		if err != nil {
			return fmt.Errorf("channels: %w", err)
		}
	}

	// Extract analog channels for range configuration.
	var analogChannels []int
	for _, ch := range queue {
		if ch < usb1808.NumAInChannels {
			analogChannels = append(analogChannels, ch)
		}
	}

	mode, err := parseMode(c.Mode)
	if err != nil {
		return err
	}

	// Configure analog inputs.
	if len(analogChannels) > 0 {
		ranges, err := parseRanges(c.Range, len(analogChannels))
		if err != nil {
			return fmt.Errorf("range: %w", err)
		}
		if err := configureAnalogInputs(dev, analogChannels, ranges, mode); err != nil {
			return fmt.Errorf("configure: %w", err)
		}
	}

	// Set up output.
	var out *os.File
	if c.Output == "-" {
		out = os.Stdout
	} else {
		f, err := os.Create(c.Output)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		out = f
	}
	w := bufio.NewWriterSize(out, 64*1024)
	defer w.Flush()

	var flushTicker *time.Ticker
	if c.Flush > 0 {
		flushTicker = time.NewTicker(c.Flush)
		defer flushTicker.Stop()
	}

	// Configure trigger.
	var scanOpts uint8
	if c.Trigger != "none" {
		scanOpts |= usb1808.ScanOptExternalTrigger
		if err := dev.SetTriggerConfig(triggerConfigByte(c.Trigger)); err != nil {
			return fmt.Errorf("trigger: %w", err)
		}
	}
	if c.Retrigger > 0 {
		scanOpts |= usb1808.ScanOptRetriggerMode
	}

	// Stop any stale scan.
	_ = dev.StopAnalogInScan()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg := usb1808.AnalogInScanConfig{
		Channels:    queue,
		Rate:        c.Rate,
		Count:       uint32(c.Count),
		RetrigCount: c.Retrigger,
		Options:     scanOpts,
	}

	// Column names.
	colNames := make([]string, len(queue))
	for i, ch := range queue {
		colNames[i] = scanChanNames[ch]
	}

	startTime := time.Now()

	// Write header.
	switch c.Format {
	case "json":
		// No header for JSONL.
	default:
		if c.Timestamp != "none" {
			fmt.Fprintf(w, "timestamp")
		}
		for i, name := range colNames {
			if i > 0 || c.Timestamp != "none" {
				fmt.Fprint(w, ",")
			}
			fmt.Fprint(w, name)
		}
		fmt.Fprintln(w)
	}

	scan := 0
	for frame, err := range dev.ScanAnalogIn(ctx, cfg) {
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}

		elapsed := float64(scan) / c.Rate

		switch c.Format {
		case "json":
			obj := make(map[string]any, len(queue)+2)
			obj["scan"] = scan
			if c.Timestamp != "none" {
				obj["timestamp"] = c.formatTimestamp(elapsed, startTime)
			}
			for i, name := range colNames {
				obj[name] = frame[i]
			}
			if err := printJSON(obj); err != nil {
				return err
			}

		default:
			if c.Timestamp != "none" {
				fmt.Fprint(w, c.formatTimestamp(elapsed, startTime))
			}
			for i, v := range frame {
				if i > 0 || c.Timestamp != "none" {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, "%.6f", v)
			}
			fmt.Fprintln(w)
		}

		if flushTicker != nil && tickFired(flushTicker) {
			w.Flush()
		}

		scan++
	}
	return nil
}

func tickFired(t *time.Ticker) bool {
	select {
	case <-t.C:
		return true
	default:
		return false
	}
}

func (c *analogScanCmd) formatTimestamp(elapsed float64, startTime time.Time) string {
	switch c.Timestamp {
	case "unix":
		return fmt.Sprintf("%.6f", float64(startTime.UnixNano())/1e9+elapsed)
	case "iso8601":
		t := startTime.Add(time.Duration(elapsed * float64(time.Second)))
		return t.Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%.6f", elapsed)
	}
}

