package main

import "fmt"

type infoCmd struct {
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *infoCmd) Run(app *cli) error {
	dev, err := openDevice(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	model := dev.Model()
	serial, err := dev.SerialNumber()
	if err != nil {
		return fmt.Errorf("serial: %w", err)
	}

	status, err := dev.Status()
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}

	major, minor, err := dev.FPGAVersion()
	if err != nil {
		return fmt.Errorf("FPGA version: %w", err)
	}

	if c.Format == "json" {
		info := map[string]any{
			"model":         model.String(),
			"serial":        serial,
			"fpga_version":  fmt.Sprintf("%d.%d", major, minor),
			"status_raw":    fmt.Sprintf("0x%04X", uint16(status)),
			"fpga_configured": status.FPGAConfigured(),
		}

		if err := dev.Init(); err == nil {
			calDate, calErr := dev.CalibrationDate()
			if calErr == nil {
				info["cal_date"] = calDate.Format("2006-01-02T15:04:05Z")
			}
		}
		return printJSON(info)
	}

	fmt.Printf("Model:   %s\n", model)
	fmt.Printf("Serial:  %s\n", serial)
	fmt.Printf("FPGA:    %d.%d\n", major, minor)
	fmt.Printf("Status:  0x%04X (FPGA configured: %v)\n", uint16(status), status.FPGAConfigured())

	if err := dev.Init(); err != nil {
		fmt.Printf("Init:    %v\n", err)
		return nil
	}

	calDate, err := dev.CalibrationDate()
	if err != nil {
		return fmt.Errorf("cal date: %w", err)
	}
	fmt.Printf("Cal:     %s\n", calDate.Format("2006-01-02 15:04:05"))
	return nil
}
