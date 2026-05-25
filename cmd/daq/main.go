// Command daq is a CLI tool for interacting with MCC USB-1808/1808X DAQ devices.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/borud/mcc-usb-1808/v4/device"
)

var (
	version   = "dev"
	buildDate = "unknown"
)

type cli struct {
	LogLevel  string `help:"Log level (${enum})." default:"info" enum:"debug,info,warn,error"`
	LogFormat string `help:"Log format (${enum})." default:"text" enum:"text,json"`
	Model  string `help:"Force device model (1808,1808x)." default:"" enum:",1808,1808x"`
	Device string `help:"Select device by USB address (bus:addr, e.g. 1:5)." default:"" short:"d"`

	List    listCmd    `cmd:"" help:"List connected devices."`
	Info    infoCmd    `cmd:"" help:"Show device information."`
	Status  statusCmd  `cmd:"" help:"Show device status."`
	Reset   resetCmd   `cmd:"" help:"Reset the device."`
	Blink   blinkCmd   `cmd:"" help:"Blink the device LED."`
	Version versionCmd `cmd:"" help:"Show version information."`
	Cal     calCmd     `cmd:"" help:"Calibration data."`
	Capture captureCmd `cmd:"" help:"Capture scan data to directory."`
	File    fileCmd    `cmd:"" help:"Capture file operations."`
	Bench   benchCmd   `cmd:"" help:"Benchmark scan throughput."`
}

func main() {
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

func parseDevice(s string) (bus, addr uint8, err error) {
	busStr, addrStr, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("invalid device address %q: expected bus:addr (e.g. 1:5)", s)
	}
	b, err := strconv.ParseUint(busStr, 10, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid bus %q: %w", busStr, err)
	}
	a, err := strconv.ParseUint(addrStr, 10, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid addr %q: %w", addrStr, err)
	}
	return uint8(b), uint8(a), nil
}

func openDevice(app *cli) (*device.Device, error) {
	var opts []device.Option
	switch {
	case app.Device != "":
		bus, addr, err := parseDevice(app.Device)
		if err != nil {
			return nil, err
		}
		opts = append(opts, device.AtAddress(bus, addr))
	case app.Model == "1808":
		opts = append(opts, device.WithModel(device.USB1808))
	case app.Model == "1808x":
		opts = append(opts, device.WithModel(device.USB1808X))
	}

	dev, err := device.Open(opts...)
	if err != nil {
		return nil, err
	}
	dev.SetLogger(setupLogger(app.LogLevel, app.LogFormat))
	return dev, nil
}

func openAndInit(app *cli) (*device.Device, error) {
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
