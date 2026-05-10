package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/borud/mcc-usb-1808"
	"github.com/borud/mcc-usb-1808/capture"
)

type captureCmd struct {
	Channels    string `help:"Analog input channels (e.g. 0-3 or 0,2,4)." default:"0-3"`
	Queue       string `help:"Mixed scan queue (e.g. ain0,ain1,dio,counter0)." default:""`
	Range       string `help:"Voltage range (bp10v,bp5v,up10v,up5v)." default:"bp10v"`
	Mode        string `help:"Input mode (${enum})." default:"differential" enum:"differential,single-ended,grounded"`
	Rate        float64 `help:"Sample rate in Hz per channel." default:"10000"`
	Count       int     `help:"Number of scans (0=continuous)." default:"0"`
	Trigger     string  `help:"Trigger mode (${enum})." default:"none" enum:"none,rising,falling,high,low"`
	Retrigger   uint32  `help:"Scans per trigger event (0=disabled)." default:"0"`
	Out         string  `help:"Output file (default: capture_<timestamp>.daq)." short:"o"`
	Compress    bool    `help:"Enable zstd compression." default:"false"`
	Raw         bool    `help:"Store raw uint32 values instead of calibrated float64." default:"false"`
	BufferSize  int     `help:"Frames to buffer before flushing." default:"1024"`
	Description string  `help:"Free-form description stored in capture header." default:""`
	Operator    string  `help:"Operator name stored in capture header." default:""`
	SessionID   string  `help:"Session identifier stored in capture header." default:""`
}

func (c *captureCmd) Run(app *cli) error {
	if c.Out == "" {
		c.Out = fmt.Sprintf("capture_%s.daq", time.Now().Format("20060102_150405"))
	}

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

	ranges, err := parseRanges(c.Range, max(len(analogChannels), 1))
	if err != nil {
		return fmt.Errorf("range: %w", err)
	}

	if len(analogChannels) > 0 {
		if err := configureAnalogInputs(dev, analogChannels, ranges, mode); err != nil {
			return fmt.Errorf("configure: %w", err)
		}
	}

	// Build capture header.
	header, err := c.buildHeader(dev, queue, ranges)
	if err != nil {
		return fmt.Errorf("build header: %w", err)
	}

	// Open output file.
	f, err := os.Create(c.Out)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	// Create capture writer.
	var writerOpts []capture.WriterOption
	if c.Compress {
		writerOpts = append(writerOpts, capture.WithCompression(true))
	}
	if c.BufferSize > 0 {
		writerOpts = append(writerOpts, capture.WithBufferSize(c.BufferSize))
	}

	cw, err := capture.NewWriter(f, header, writerOpts...)
	if err != nil {
		return fmt.Errorf("create writer: %w", err)
	}
	defer cw.Close()

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

	_ = dev.StopAnalogInScan()

	// Handle interrupt: first SIGINT stops the scan gracefully,
	// subsequent signals are ignored until data is flushed.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nstopping capture, flushing data...\n")
		cancel()
		// Ignore further signals so we don't get killed during flush.
		signal.Reset(os.Interrupt)
	}()

	cfg := usb1808.AnalogInScanConfig{
		Channels:    queue,
		Rate:        c.Rate,
		Count:       uint32(c.Count),
		RetrigCount: c.Retrigger,
		Options:     scanOpts,
	}

	fmt.Fprintf(os.Stderr, "capturing to %s (%d channels, %.0f Hz, press Ctrl-C to stop)\n", c.Out, len(queue), c.Rate)

	if c.Raw {
		for frame, err := range dev.ScanAnalogInRaw(ctx, cfg) {
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			if err := cw.WriteFrame(frame); err != nil {
				return fmt.Errorf("write: %w", err)
			}
		}
	} else {
		for frame, err := range dev.ScanAnalogIn(ctx, cfg) {
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			if err := cw.WriteFrameFloat64(frame); err != nil {
				return fmt.Errorf("write: %w", err)
			}
		}
	}

	if err := cw.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d frames\n", cw.FramesWritten())
	return nil
}

func (c *captureCmd) buildHeader(dev *usb1808.Device, queue []int, ranges []usb1808.Range) (capture.Header, error) {
	serial, err := dev.SerialNumber()
	if err != nil {
		return capture.Header{}, fmt.Errorf("serial: %w", err)
	}

	major, minor, err := dev.FPGAVersion()
	if err != nil {
		return capture.Header{}, fmt.Errorf("fpga version: %w", err)
	}

	calDate, _ := dev.CalibrationDate()
	calTable := dev.AnalogInCalTable()

	// Build channel descriptors.
	channels := make([]capture.Channel, len(queue))
	analogIdx := 0
	for i, ch := range queue {
		cc := capture.Channel{
			Index: ch,
			Name:  scanChanNames[ch],
		}
		switch {
		case ch < usb1808.NumAInChannels:
			cc.Type = capture.AnalogIn
			r := ranges[analogIdx]
			cc.Range = uint8(r)
			cal := calTable[ch][r] // #nosec G602 -- ch and r validated by scan queue config
			cc.Cal = &capture.CalEntry{
				Slope:  cal.Slope,
				Offset: cal.Offset,
			}
			analogIdx++
		case ch == 8:
			cc.Type = capture.DigitalIO
		case ch <= 10:
			cc.Type = capture.Counter
		default:
			cc.Type = capture.Encoder
		}
		channels[i] = cc
	}

	format := capture.CalibratedFloat64
	if c.Raw {
		format = capture.RawUint32
	}

	h := capture.Header{
		DeviceModel:     dev.Model().String(),
		DeviceSerial:    serial,
		FPGAVersion:     fmt.Sprintf("%d.%d", major, minor),
		CalibrationDate: calDate,
		Channels:        channels,
		SampleRate:      c.Rate,
		Format:          format,
		Timestamp:       time.Now().UnixMilli(),
	}

	if c.Description != "" {
		h.Description = c.Description
	}
	if c.Operator != "" {
		h.Operator = c.Operator
	}
	if c.SessionID != "" {
		h.SessionID = c.SessionID
	}

	return h, nil
}
