// Command daq is a CLI tool for interacting with MCC USB-1808/1808X DAQ devices.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	"github.com/borud/mcc-usb-1808"
)

var (
	version   = "dev"
	buildDate = "unknown"
)

type cli struct {
	LogLevel  string `help:"Log level (${enum})." default:"info" enum:"debug,info,warn,error"`
	LogFormat string `help:"Log format (${enum})." default:"text" enum:"text,json"`
	Model     string `help:"Force device model (1808,1808x)." default:"" enum:",1808,1808x"`

	Info    infoCmd    `cmd:"" help:"Show device information."`
	Status  statusCmd  `cmd:"" help:"Show device status."`
	Reset   resetCmd   `cmd:"" help:"Reset the device."`
	Blink   blinkCmd   `cmd:"" help:"Blink the device LED."`
	Version versionCmd `cmd:"" help:"Show version information."`

	Analog  analogCmd  `cmd:"" help:"Analog I/O commands."`
	Dio     dioCmd     `cmd:"" help:"Digital I/O commands."`
	Counter counterCmd `cmd:"" help:"Counter and encoder commands."`
	Timer   timerCmd   `cmd:"" help:"Timer/PWM commands."`
	Trigger triggerCmd `cmd:"" help:"Trigger configuration."`
	Pattern patternCmd `cmd:"" help:"Pattern detection configuration."`
	Cal     calCmd     `cmd:"" help:"Calibration data."`
	Capture captureCmd `cmd:"" help:"Capture scan data to directory."`
	File    fileCmd    `cmd:"" help:"Capture file operations."`
	Bench   benchCmd   `cmd:"" help:"Benchmark scan throughput."`
}

func main() {
	// Show help when invoked without arguments.
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "--help")
	}

	var app cli
	ctx := kong.Parse(&app,
		kong.Name("daq"),
		kong.Description("MCC USB-1808/1808X DAQ tool"),
		kong.ConfigureHelp(kong.HelpOptions{
			Compact:             true,
			NoExpandSubcommands: true,
		}),
	)
	if err := ctx.Run(&app); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func setupLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

func openDevice(app *cli) (*usb1808.Device, error) {
	var dev *usb1808.Device
	var err error
	switch app.Model {
	case "1808":
		dev, err = usb1808.OpenModel(usb1808.USB1808)
	case "1808x":
		dev, err = usb1808.OpenModel(usb1808.USB1808X)
	default:
		dev, err = usb1808.Open()
	}
	if err != nil {
		return nil, err
	}
	dev.SetLogger(setupLogger(app.LogLevel, app.LogFormat))
	return dev, nil
}

func openAndInit(app *cli) (*usb1808.Device, error) {
	dev, err := openDevice(app)
	if err != nil {
		return nil, err
	}
	if err := dev.Init(); err != nil {
		dev.Close()
		return nil, err
	}
	return dev, nil
}
