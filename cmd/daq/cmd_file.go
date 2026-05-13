package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/borud/mcc-usb-1808/capture"
	"github.com/borud/mcc-usb-1808/capture/export"
)

type fileCmd struct {
	Info   fileInfoCmd   `cmd:"" help:"Show capture directory information."`
	Export fileExportCmd `cmd:"" help:"Export capture to another format."`
}

// fileInfoCmd displays metadata from a capture directory.
type fileInfoCmd struct {
	Dir string `arg:"" help:"Capture directory to inspect."`
}

func (c *fileInfoCmd) Run(_ *cli) error {
	r, err := capture.NewReader(c.Dir)
	if err != nil {
		return err
	}
	defer r.Close()

	h := r.Header()

	// Aggregate overview.
	fmt.Printf("Directory:       %s\n", c.Dir)
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

	// Per-segment table.
	fmt.Println("\nSegments:")
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return err
	}
	fmt.Printf("  %-6s  %-12s  %-14s  %s\n", "Seq", "Frames", "Frame Offset", "Size")
	var totalSize int64
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "seg_") || !strings.HasSuffix(e.Name(), ".daq") {
			continue
		}
		if strings.HasSuffix(e.Name(), ".pending") {
			continue
		}
		path := filepath.Join(c.Dir, e.Name())
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		totalSize += fi.Size()

		f, err := os.Open(path) // #nosec G304 -- CLI tool, path from user args
		if err != nil {
			continue
		}
		pre, err := readPreamble(f)
		f.Close()
		if err != nil {
			continue
		}
		fmt.Printf("  %-6d  %-12d  %-14d  %s\n",
			pre.sequenceNumber, pre.frameCount, pre.globalFrameOffset, humanBytes(fi.Size()))
	}
	fmt.Printf("\nTotal size:      %s\n", humanBytes(totalSize))

	return nil
}

// readPreamble reads the capture preamble from an open file.
func readPreamble(f *os.File) (struct {
	sequenceNumber    uint16
	frameCount        uint64
	globalFrameOffset uint64
}, error) {
	type result struct {
		sequenceNumber    uint16
		frameCount        uint64
		globalFrameOffset uint64
	}

	buf := make([]byte, 28) // preambleLen
	if _, err := f.Read(buf); err != nil {
		return result{}, err
	}

	return result{
		sequenceNumber:    uint16(buf[18]) | uint16(buf[19])<<8,
		frameCount:        uint64(buf[10]) | uint64(buf[11])<<8 | uint64(buf[12])<<16 | uint64(buf[13])<<24 | uint64(buf[14])<<32 | uint64(buf[15])<<40 | uint64(buf[16])<<48 | uint64(buf[17])<<56,
		globalFrameOffset: uint64(buf[20]) | uint64(buf[21])<<8 | uint64(buf[22])<<16 | uint64(buf[23])<<24 | uint64(buf[24])<<32 | uint64(buf[25])<<40 | uint64(buf[26])<<48 | uint64(buf[27])<<56,
	}, nil
}

// fileExportCmd exports a capture to another format.
type fileExportCmd struct {
	ExportFormat string `help:"Export format (${enum})." enum:"csv,excel,sqlite,wav,parquet" required:"" name:"to"`
	Out          string `help:"Output file path." short:"o"`
	Overwrite    bool   `help:"Overwrite existing output file." default:"false"`
	Raw          bool   `help:"Include raw sample columns where supported." default:"false"`
	Dir          string `arg:"" help:"Capture directory to export."`
}

var formatExtensions = map[string]string{
	"csv":     ".csv",
	"excel":   ".xlsx",
	"sqlite":  ".db",
	"wav":     ".wav",
	"parquet": ".parquet",
}

func (c *fileExportCmd) Run(_ *cli) error {
	// Determine output path.
	outPath := c.Out
	if outPath == "" {
		base := filepath.Base(c.Dir)
		outPath = base + formatExtensions[c.ExportFormat]
	}

	// Check overwrite.
	if !c.Overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("output file %s already exists (use --overwrite to replace)", outPath)
		}
	}

	r, err := capture.NewReader(c.Dir)
	if err != nil {
		return fmt.Errorf("open capture: %w", err)
	}
	defer r.Close()

	// Start progress display.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var progress atomic.Int64
	go func() {
		showExportProgress(ctx, "exporting", &progress)
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
	case "parquet":
		out, err := os.Create(outPath) // #nosec G304 -- CLI tool, path from user args
		if err != nil {
			cancel()
			<-done
			return err
		}
		var opts []export.ParquetOption
		if c.Raw {
			opts = append(opts, export.WithRaw())
		}
		exportErr = export.Parquet(out, r, opts...)
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

// showExportProgress displays a spinner on stderr until ctx is cancelled.
func showExportProgress(ctx context.Context, label string, _ *atomic.Int64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	spinner := []rune{'|', '/', '-', '\\'}
	i := 0

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "\r%s... done\n", label)
			return
		case <-ticker.C:
			fmt.Fprintf(os.Stderr, "\r%s %c", label, spinner[i%len(spinner)])
			i++
		}
	}
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
		return "RawUint32 (4 bytes/sample, little-endian)"
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
