package main

import "fmt"

type statusCmd struct {
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *statusCmd) Run(app *cli) error {
	dev, err := openDevice(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	status, err := dev.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}

	if c.Format == "json" {
		return printJSON(map[string]any{
			"raw":               fmt.Sprintf("0x%04X", uint16(status)),
			"fpga_configured":   status.FPGAConfigured(),
			"fpga_config_mode":  status.FPGAConfigMode(),
			"ain_scan_running":  status.AInScanRunning(),
			"ain_scan_overrun":  status.AInScanOverrun(),
			"ain_scan_done":     status.AInScanDone(),
			"aout_scan_running": status.AOutScanRunning(),
			"aout_scan_underrun": status.AOutScanUnderrun(),
			"aout_scan_done":    status.AOutScanDone(),
		})
	}

	fmt.Printf("Status: 0x%04X\n", uint16(status))
	fmt.Printf("  FPGA configured:       %v\n", status.FPGAConfigured())
	fmt.Printf("  FPGA config mode:      %v\n", status.FPGAConfigMode())
	fmt.Printf("  AIn scan running:      %v\n", status.AInScanRunning())
	fmt.Printf("  AIn scan overrun:      %v\n", status.AInScanOverrun())
	fmt.Printf("  AIn scan done:         %v\n", status.AInScanDone())
	fmt.Printf("  AOut scan running:     %v\n", status.AOutScanRunning())
	fmt.Printf("  AOut scan underrun:    %v\n", status.AOutScanUnderrun())
	fmt.Printf("  AOut scan done:        %v\n", status.AOutScanDone())
	return nil
}
