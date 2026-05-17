package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/borud/mcc-usb-1808/v4/device"
)

var queueNames = map[string]int{
	"ain0": 0, "ain1": 1, "ain2": 2, "ain3": 3,
	"ain4": 4, "ain5": 5, "ain6": 6, "ain7": 7,
	"dio":      8,
	"counter0": 9, "counter1": 10,
	"encoder0": 11, "encoder1": 12,
}

var scanChanNames = map[int]string{
	0: "ain0", 1: "ain1", 2: "ain2", 3: "ain3",
	4: "ain4", 5: "ain5", 6: "ain6", 7: "ain7",
	8:  "dio",
	9:  "counter0", 10: "counter1",
	11: "encoder0", 12: "encoder1",
}

var rangeNames = map[string]device.Range{
	"bp10v": device.BP10V,
	"bp5v":  device.BP5V,
	"up10v": device.UP10V,
	"up5v":  device.UP5V,
}

var modeNames = map[string]device.InputMode{
	"diff":         device.Differential,
	"differential": device.Differential,
	"se":           device.SingleEnded,
	"single-ended": device.SingleEnded,
	"grounded":     device.Grounded,
}

// parseChannelSpec parses a channel specification like:
// "ain0-ain3:bp10v:diff,dio,counter0"
// "ain0:bp5v,ain1:up10v:se,dio"
// "all" (all 8 analog + dio)
// "analog" (ain0-ain7)
func parseChannelSpec(spec string, defaultRange device.Range, defaultMode device.InputMode) ([]device.ChannelConfig, error) {
	spec = strings.TrimSpace(spec)

	switch strings.ToLower(spec) {
	case "all":
		var channels []device.ChannelConfig
		for i := range device.NumAInChannels {
			channels = append(channels, device.ChannelConfig{
				Index: i, Type: device.ChannelTypeAnalog,
				Range: defaultRange, Mode: defaultMode,
			})
		}
		channels = append(channels, device.ChannelConfig{
			Index: device.ScanChanDIO, Type: device.ChannelTypeDIO,
		})
		return channels, nil
	case "analog":
		var channels []device.ChannelConfig
		for i := range device.NumAInChannels {
			channels = append(channels, device.ChannelConfig{
				Index: i, Type: device.ChannelTypeAnalog,
				Range: defaultRange, Mode: defaultMode,
			})
		}
		return channels, nil
	}

	var channels []device.ChannelConfig
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split on ":" for per-channel options.
		fields := strings.Split(part, ":")
		name := strings.ToLower(fields[0])

		// Check for range syntax "ain0-ain3".
		if strings.Contains(name, "-") && !strings.HasPrefix(name, "-") {
			expanded, err := expandRange(name)
			if err != nil {
				return nil, err
			}
			r := defaultRange
			m := defaultMode
			if len(fields) > 1 {
				if rr, ok := rangeNames[strings.ToLower(fields[1])]; ok {
					r = rr
				}
			}
			if len(fields) > 2 {
				if mm, ok := modeNames[strings.ToLower(fields[2])]; ok {
					m = mm
				}
			}
			for _, idx := range expanded {
				channels = append(channels, makeChannelConfig(idx, r, m))
			}
			continue
		}

		idx, ok := queueNames[name]
		if !ok {
			n, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("unknown channel: %s", name)
			}
			idx = n
		}

		r := defaultRange
		m := defaultMode
		if len(fields) > 1 {
			if rr, ok := rangeNames[strings.ToLower(fields[1])]; ok {
				r = rr
			}
		}
		if len(fields) > 2 {
			if mm, ok := modeNames[strings.ToLower(fields[2])]; ok {
				m = mm
			}
		}
		channels = append(channels, makeChannelConfig(idx, r, m))
	}

	if len(channels) == 0 {
		return nil, fmt.Errorf("no channels specified")
	}
	return channels, nil
}

func expandRange(s string) ([]int, error) {
	parts := strings.SplitN(s, "-", 2)
	loIdx, okLo := queueNames[parts[0]]
	hiIdx, okHi := queueNames[parts[1]]
	if !okLo || !okHi {
		return nil, fmt.Errorf("invalid range: %s", s)
	}
	var result []int
	for i := loIdx; i <= hiIdx; i++ {
		result = append(result, i)
	}
	return result, nil
}

func makeChannelConfig(idx int, r device.Range, m device.InputMode) device.ChannelConfig {
	switch {
	case idx < device.NumAInChannels:
		return device.ChannelConfig{Index: idx, Type: device.ChannelTypeAnalog, Range: r, Mode: m}
	case idx == device.ScanChanDIO:
		return device.ChannelConfig{Index: idx, Type: device.ChannelTypeDIO}
	case idx <= device.ScanChanCounter1:
		return device.ChannelConfig{Index: idx, Type: device.ChannelTypeCounter}
	default:
		return device.ChannelConfig{Index: idx, Type: device.ChannelTypeEncoder}
	}
}

// printJSON encodes v as indented JSON and writes to stdout.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// triggerConfigByte converts a trigger mode string to a config byte.
func triggerConfigByte(mode string) uint8 {
	switch mode {
	case "rising":
		return device.TriggerEdge | device.TriggerHigh
	case "falling":
		return device.TriggerEdge
	case "high":
		return device.TriggerHigh
	case "low":
		return 0
	default:
		return 0
	}
}

// parseHumanInt parses an integer with optional k/K/M suffix.
func parseHumanInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	multiplier := 1
	if strings.HasSuffix(s, "k") || strings.HasSuffix(s, "K") {
		multiplier = 1000
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "M") {
		multiplier = 1_000_000
		s = s[:len(s)-1]
	}
	// Support underscores.
	s = strings.ReplaceAll(s, "_", "")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n * multiplier, nil
}
