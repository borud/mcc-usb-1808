package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/borud/mcc-usb-1808/capture"
	"github.com/borud/mcc-usb-1808/capture/export"
)

type fileCmd struct {
	Info   fileInfoCmd   `cmd:"" help:"Show capture file information."`
	Export fileExportCmd `cmd:"" help:"Export capture file to another format."`
}

// fileInfoCmd displays metadata from a capture file.
type fileInfoCmd struct {
	File string `arg:"" help:"Capture file to inspect."`
}

func (c *fileInfoCmd) Run(_ *cli) error {
	fi, err := os.Stat(c.File)
	if err != nil {
		return err
	}

	f, err := os.Open(c.File)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read preamble to get raw header length and flags.
	preamble := make([]byte, 18)
	if _, err := io.ReadFull(f, preamble); err != nil {
		return fmt.Errorf("read preamble: %w", err)
	}

	flags := preamble[5]
	headerLen := binary.LittleEndian.Uint32(preamble[6:10])
	compressed := flags&0x01 != 0

	// Rewind and open with Reader to get the parsed header.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	r, err := capture.NewReader(f)
	if err != nil {
		return err
	}
	defer r.Close()

	h := r.Header()

	// File overview.
	fmt.Printf("File:            %s\n", c.File)
	fmt.Printf("Size:            %s\n", humanBytes(fi.Size()))
	fmt.Printf("Header:          %d bytes (preamble: 18, JSON: %d)\n", 18+headerLen, headerLen)
	fmt.Printf("Compressed:      %v\n", compressed)
	fmt.Printf("Format:          %s\n", formatName(h.Format))
	if h.FrameCount > 0 {
		fmt.Printf("Frame count:     %d\n", h.FrameCount)
		dur := float64(h.FrameCount) / h.SampleRate
		fmt.Printf("Duration:        %.3f s\n", dur)
	}

	// Device info.
	fmt.Println()
	fmt.Printf("Device:          %s (serial: %s)\n", h.DeviceModel, h.DeviceSerial)
	if h.FPGAVersion != "" {
		fmt.Printf("FPGA version:    %s\n", h.FPGAVersion)
	}
	if !h.CalibrationDate.IsZero() {
		fmt.Printf("Calibration:     %s\n", h.CalibrationDate.Format("2006-01-02"))
	}
	fmt.Printf("Sample rate:     %g Hz\n", h.SampleRate)

	// Channels.
	fmt.Printf("\nChannels (%d):\n", len(h.Channels))
	for i, ch := range h.Channels {
		label := ch.Name
		if label == "" {
			label = fmt.Sprintf("ch%d", i)
		}
		line := fmt.Sprintf("  [%d] %-12s %s", ch.Index, label, channelTypeName(ch.Type))
		if ch.Type == capture.AnalogIn {
			line += fmt.Sprintf("  range=%s", rangeName(ch.Range))
		}
		if ch.Cal != nil {
			line += fmt.Sprintf("  cal: slope=%g, offset=%g", ch.Cal.Slope, ch.Cal.Offset)
		}
		fmt.Println(line)
	}

	// Session metadata.
	hasSession := h.Timestamp > 0 || h.ApplicationName != "" || h.SessionID != "" || h.Operator != "" || h.Description != ""
	if hasSession {
		fmt.Println()
		if h.Timestamp > 0 {
			t := time.UnixMilli(h.Timestamp)
			fmt.Printf("Timestamp:       %s\n", t.Format(time.RFC3339))
		}
		if h.ApplicationName != "" {
			fmt.Printf("Application:     %s\n", h.ApplicationName)
		}
		if h.SessionID != "" {
			fmt.Printf("Session ID:      %s\n", h.SessionID)
		}
		if h.Operator != "" {
			fmt.Printf("Operator:        %s\n", h.Operator)
		}
		if h.Description != "" {
			fmt.Printf("Description:     %s\n", h.Description)
		}
	}

	// Properties.
	if len(h.Properties) > 0 {
		fmt.Println("\nProperties:")
		for k, v := range h.Properties {
			fmt.Printf("  %-16s %s\n", k+":", v)
		}
	}

	return nil
}

// fileExportCmd exports a capture file to another format.
type fileExportCmd struct {
	ExportFormat string `help:"Export format (${enum})." enum:"csv,excel,sqlite,wav" required:"" name:"to"`
	Out          string `help:"Output file path." short:"o"`
	Overwrite    bool   `help:"Overwrite existing output file." default:"false"`
	File         string `arg:"" help:"Capture file to export."`
}

var formatExtensions = map[string]string{
	"csv":    ".csv",
	"excel":  ".xlsx",
	"sqlite": ".db",
	"wav":    ".wav",
}

func (c *fileExportCmd) Run(_ *cli) error {
	// Determine output path.
	outPath := c.Out
	if outPath == "" {
		base := strings.TrimSuffix(filepath.Base(c.File), filepath.Ext(c.File))
		outPath = base + formatExtensions[c.ExportFormat]
	}

	// Check overwrite.
	if !c.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("output file %s already exists (use --overwrite to replace)", outPath)
		}
	}

	// Open capture file with a counting reader for progress.
	fi, err := os.Stat(c.File)
	if err != nil {
		return err
	}

	f, err := os.Open(c.File)
	if err != nil {
		return err
	}
	defer f.Close()

	cr := &countingReader{r: f}
	r, err := capture.NewReader(cr)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	defer r.Close()

	// Start progress bar.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		showProgress(ctx, "exporting", cr, fi.Size())
		close(done)
	}()

	// Run export.
	var exportErr error
	switch c.ExportFormat {
	case "csv":
		out, err := os.Create(outPath) // #nosec G304 -- CLI tool, path from user args
		if err != nil {
			cancel()
			<-done
			return err
		}
		exportErr = export.CSV(out, r)
		if closeErr := out.Close(); closeErr != nil && exportErr == nil {
			exportErr = closeErr
		}
	case "excel":
		exportErr = export.Excel(outPath, r)
	case "sqlite":
		exportErr = export.SQLite(outPath, r)
	case "wav":
		out, err := os.Create(outPath) // #nosec G304 -- CLI tool, path from user args
		if err != nil {
			cancel()
			<-done
			return err
		}
		exportErr = export.WAV(out, r)
		if closeErr := out.Close(); closeErr != nil && exportErr == nil {
			exportErr = closeErr
		}
	}

	cancel()
	<-done

	if exportErr != nil {
		os.Remove(outPath)
		return fmt.Errorf("export: %w", exportErr)
	}

	outInfo, _ := os.Stat(outPath)
	if outInfo != nil {
		fmt.Fprintf(os.Stderr, "wrote %s (%s)\n", outPath, humanBytes(outInfo.Size()))
	} else {
		fmt.Fprintf(os.Stderr, "wrote %s\n", outPath)
	}

	return nil
}

// countingReader wraps an io.Reader and counts bytes read.
type countingReader struct {
	r     io.Reader
	count atomic.Int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.count.Add(int64(n))
	return n, err
}

// showProgress displays a progress bar on stderr until ctx is cancelled.
func showProgress(ctx context.Context, label string, cr *countingReader, total int64) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			printProgress(label, total, total)
			fmt.Fprintln(os.Stderr)
			return
		case <-ticker.C:
			printProgress(label, cr.count.Load(), total)
		}
	}
}

func printProgress(label string, current, total int64) {
	const barWidth = 30
	pct := float64(current) / float64(total)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * barWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Fprintf(os.Stderr, "\r%s [%s] %3.0f%%", label, bar, pct*100)
}

// Helper formatting functions.

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatName(f capture.DataFormat) string {
	switch f {
	case capture.RawUint32:
		return "RawUint32 (4 bytes/sample)"
	case capture.CalibratedFloat64:
		return "CalibratedFloat64 (8 bytes/sample)"
	default:
		return fmt.Sprintf("unknown(%d)", f)
	}
}

func channelTypeName(t capture.ChannelType) string {
	switch t {
	case capture.AnalogIn:
		return "AnalogIn"
	case capture.DigitalIO:
		return "DigitalIO"
	case capture.Counter:
		return "Counter"
	case capture.Encoder:
		return "Encoder"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

func rangeName(r uint8) string {
	switch r {
	case 0:
		return "BP10V"
	case 1:
		return "BP5V"
	case 2:
		return "UP10V"
	case 3:
		return "UP5V"
	default:
		return fmt.Sprintf("range(%d)", r)
	}
}
