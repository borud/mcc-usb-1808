package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/borud/mcc-usb-1808/v4/capture"
	"github.com/borud/mcc-usb-1808/v4/device"
)

type captureCmd struct {
	Channels    string  `help:"Channel spec (e.g. ain0-ain3:bp10v:diff,dio,counter0)." default:"analog"`
	Range       string  `help:"Default voltage range (bp10v,bp5v,up10v,up5v)." default:"bp10v"`
	Mode        string  `help:"Default input mode (${enum})." default:"diff" enum:"diff,differential,se,single-ended,grounded"`
	Rate        string  `help:"Sample rate in Hz per channel (supports k/M suffix)." default:"10000"`
	Count       int     `help:"Number of scans (0=continuous)." default:"0"`
	Trigger     string  `help:"Trigger mode (${enum})." default:"none" enum:"none,rising,falling,high,low"`
	Retrigger   uint32  `help:"Scans per trigger event (0=disabled)." default:"0"`
	Out         string  `help:"Output directory (default: capture_<timestamp>)." short:"o"`
	FileSize    int     `help:"Target segment file size in bytes." default:"104857600"`
	BufferSize  int     `help:"Frames to buffer before flushing." default:"8192"`
	Pipeline    int     `help:"USB read-ahead pipeline depth (batches buffered)." default:"32"`
	Description string  `help:"Free-form description stored in capture header." default:""`
	Operator    string  `help:"Operator name stored in capture header." default:""`
	SessionID   string  `help:"Session identifier stored in capture header." default:""`
	CPUProfile  string  `help:"Write CPU profile to file." default:""`
}

func (c *captureCmd) Run(app *cli) error {
	if c.CPUProfile != "" {
		f, err := os.Create(c.CPUProfile)
		if err != nil {
			return fmt.Errorf("create cpu profile: %w", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("start cpu profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if c.Out == "" {
		c.Out = fmt.Sprintf("capture_%s", time.Now().Format("20060102_150405"))
	}

	rate, err := parseHumanInt(c.Rate)
	if err != nil {
		return fmt.Errorf("rate: %w", err)
	}

	defaultRange := rangeNames[c.Range]
	defaultMode := modeNames[c.Mode]

	channels, err := parseChannelSpec(c.Channels, defaultRange, defaultMode)
	if err != nil {
		return fmt.Errorf("channels: %w", err)
	}

	dev, err := openAndInit(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	// Build scan options.
	var scanOpts uint8
	if c.Trigger != "none" {
		scanOpts |= device.ScanOptExternalTrigger
		if err := dev.SetTriggerConfig(triggerConfigByte(c.Trigger)); err != nil {
			return fmt.Errorf("trigger: %w", err)
		}
	}
	if c.Retrigger > 0 {
		scanOpts |= device.ScanOptRetriggerMode
	}

	cfg := device.ScanConfig{
		Channels: channels,
		Rate:     rate,
		Count:    uint32(c.Count),
		Options:  scanOpts,
	}

	handle, err := dev.CreateScan(cfg, device.WithPipelineDepth(c.Pipeline))
	if err != nil {
		return fmt.Errorf("create scan: %w", err)
	}

	// Build capture header.
	header, err := c.buildHeader(dev, channels)
	if err != nil {
		return fmt.Errorf("build header: %w", err)
	}

	// Create capture writer.
	var writerOpts []capture.WriterOption
	if c.FileSize > 0 {
		writerOpts = append(writerOpts, capture.WithFileSize(c.FileSize))
	}
	if c.BufferSize > 0 {
		writerOpts = append(writerOpts, capture.WithBufferSize(c.BufferSize))
	}

	cw, err := capture.NewWriter(c.Out, header, writerOpts...)
	if err != nil {
		return fmt.Errorf("create writer: %w", err)
	}
	defer cw.Close()

	// Handle interrupt.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nstopping capture, flushing data...\n")
		handle.Stop()
		signal.Reset(os.Interrupt)
	}()

	if err := handle.Start(); err != nil {
		return fmt.Errorf("start scan: %w", err)
	}

	fmt.Fprintf(os.Stderr, "capturing to %s/ (%d channels, %d Hz, press Ctrl-C to stop)\n",
		c.Out, len(channels), rate)

	// Decouple disk I/O from USB: scan loop copies into channel, writer goroutine flushes.
	const writeDepth = 64
	writeCh := make(chan []byte, writeDepth)
	writePool := make(chan []byte, writeDepth+1)

	var writeErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for data := range writeCh {
			if writeErr == nil {
				if err := cw.WriteBulk(data); err != nil {
					writeErr = err
				}
			}
			writePool <- data[:cap(data)]
		}
	}()

	for data := range handle.Chunks() {
		var buf []byte
		select {
		case buf = <-writePool:
		default:
			buf = make([]byte, len(data))
		}
		buf = buf[:len(data)]
		copy(buf, data)
		writeCh <- buf
	}
	close(writeCh)
	wg.Wait()

	if err := handle.Err(); err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if writeErr != nil {
		return fmt.Errorf("write: %w", writeErr)
	}

	if err := cw.Close(); err != nil {
		return fmt.Errorf("close writer: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d frames\n", cw.FramesWritten())
	return nil
}

func (c *captureCmd) buildHeader(dev *device.Device, channels []device.ChannelConfig) (capture.Header, error) {
	serial, err := dev.SerialNumber()
	if err != nil {
		return capture.Header{}, fmt.Errorf("serial: %w", err)
	}

	major, minor, err := dev.FPGAVersion()
	if err != nil {
		return capture.Header{}, fmt.Errorf("fpga version: %w", err)
	}

	calDate, _ := dev.CalibrationDate()
	calTable := dev.CalibrationTable()

	rate, _ := parseHumanInt(c.Rate)

	chDescs := make([]capture.Channel, len(channels))
	for i, ch := range channels {
		cc := capture.Channel{
			Index: ch.Index,
			Name:  scanChanNames[ch.Index],
		}
		switch ch.Type {
		case device.ChannelTypeAnalog:
			cc.Type = capture.AnalogIn
			cc.Range = uint8(ch.Range)
			cal := calTable[ch.Index][ch.Range]
			cc.Cal = &capture.CalEntry{
				Slope:  cal.Slope,
				Offset: cal.Offset,
			}
		case device.ChannelTypeDIO:
			cc.Type = capture.DigitalIO
		case device.ChannelTypeCounter:
			cc.Type = capture.Counter
		case device.ChannelTypeEncoder:
			cc.Type = capture.Encoder
		}
		chDescs[i] = cc
	}

	h := capture.Header{
		DeviceModel:     dev.Model().String(),
		DeviceSerial:    serial,
		FPGAVersion:     fmt.Sprintf("%d.%d", major, minor),
		CalibrationDate: calDate,
		Channels:        chDescs,
		SampleRate:      float64(rate),
		Format:          capture.RawUint32,
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
