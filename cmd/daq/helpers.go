package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/borud/mcc-usb-1808/v3"
)

// parseChannels parses a channel list like "0,2,4" or "0-3" or "0-3,5,7".
func parseChannels(s string) ([]int, error) {
	var result []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid channel: %s", part)
			}
			hi, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid channel: %s", part)
			}
			for i := lo; i <= hi; i++ {
				result = append(result, i)
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid channel: %s", part)
			}
			result = append(result, n)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no channels specified")
	}
	return result, nil
}

var queueNames = map[string]int{
	"ain0": 0, "ain1": 1, "ain2": 2, "ain3": 3,
	"ain4": 4, "ain5": 5, "ain6": 6, "ain7": 7,
	"dio":      8,
	"counter0": 9, "counter1": 10,
	"encoder0": 11, "encoder1": 12,
}

// parseQueue parses a scan queue like "ain0,ain1,dio,counter0".
func parseQueue(s string) ([]int, error) {
	var result []int
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(strings.ToLower(name))
		if idx, ok := queueNames[name]; ok {
			result = append(result, idx)
		} else {
			n, err := strconv.Atoi(name)
			if err != nil {
				return nil, fmt.Errorf("unknown queue channel: %s", name)
			}
			result = append(result, n)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no queue channels specified")
	}
	return result, nil
}

var scanChanNames = map[int]string{
	0: "ain0", 1: "ain1", 2: "ain2", 3: "ain3",
	4: "ain4", 5: "ain5", 6: "ain6", 7: "ain7",
	8:  "dio",
	9:  "counter0", 10: "counter1",
	11: "encoder0", 12: "encoder1",
}

var rangeNames = map[string]usb1808.Range{
	"bp10v": usb1808.BP10V,
	"bp5v":  usb1808.BP5V,
	"up10v": usb1808.UP10V,
	"up5v":  usb1808.UP5V,
}

// parseRanges parses voltage ranges: single value or comma-separated per channel.
func parseRanges(s string, nChannels int) ([]usb1808.Range, error) {
	parts := strings.Split(s, ",")
	if len(parts) == 1 {
		r, ok := rangeNames[strings.ToLower(strings.TrimSpace(parts[0]))]
		if !ok {
			return nil, fmt.Errorf("unknown range: %s (valid: bp10v, bp5v, up10v, up5v)", parts[0])
		}
		result := make([]usb1808.Range, nChannels)
		for i := range result {
			result[i] = r
		}
		return result, nil
	}
	if len(parts) != nChannels {
		return nil, fmt.Errorf("expected 1 or %d range values, got %d", nChannels, len(parts))
	}
	result := make([]usb1808.Range, nChannels)
	for i, p := range parts {
		r, ok := rangeNames[strings.ToLower(strings.TrimSpace(p))]
		if !ok {
			return nil, fmt.Errorf("unknown range: %s", p)
		}
		result[i] = r
	}
	return result, nil
}

var modeNames = map[string]usb1808.InputMode{
	"differential": usb1808.Differential,
	"single-ended": usb1808.SingleEnded,
	"grounded":     usb1808.Grounded,
}

// parseMode parses an input mode string.
func parseMode(s string) (usb1808.InputMode, error) {
	m, ok := modeNames[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return 0, fmt.Errorf("unknown mode: %s (valid: differential, single-ended, grounded)", s)
	}
	return m, nil
}

// parseUintValue parses an integer with optional 0x (hex) or 0b (binary) prefix.
func parseUintValue(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return strconv.ParseUint(s[2:], 16, 64)
	}
	if strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B") {
		return strconv.ParseUint(s[2:], 2, 64)
	}
	return strconv.ParseUint(s, 10, 64)
}

// parseDIODirection parses pin directions: hex value or "IIOO" notation.
// I = input (bit 1), O = output (bit 0). Left-to-right is pin 3,2,1,0.
func parseDIODirection(s string) (uint16, error) {
	if v, err := parseUintValue(s); err == nil {
		return uint16(v), nil
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 4 {
		return 0, fmt.Errorf("direction must be hex value or 4 characters (I/O), got: %s", s)
	}
	var val uint16
	for i, c := range s {
		bit := 3 - i
		switch c {
		case 'I':
			val |= 1 << bit
		case 'O':
			// output = 0
		default:
			return 0, fmt.Errorf("invalid direction character: %c (use I or O)", c)
		}
	}
	return val, nil
}

// triggerConfigByte converts a trigger mode string to the config byte.
func triggerConfigByte(mode string) uint8 {
	switch mode {
	case "rising":
		return usb1808.TriggerEdge | usb1808.TriggerHigh
	case "falling":
		return usb1808.TriggerEdge
	case "high":
		return usb1808.TriggerHigh
	case "low":
		return 0
	default:
		return 0
	}
}

// counterModeByte builds a counter mode byte from individual settings.
func counterModeByte(mode, periodMult, tickSize string) (uint8, error) {
	var b uint8
	switch mode {
	case "totalize":
		b = usb1808.CounterTotalize
	case "period":
		b = usb1808.CounterPeriod
	case "pulse-width":
		b = usb1808.CounterPulseWidth
	case "timing":
		b = usb1808.CounterTiming
	default:
		return 0, fmt.Errorf("unknown counter mode: %s", mode)
	}

	switch periodMult {
	case "", "1x":
		b |= usb1808.PeriodMode1X
	case "10x":
		b |= usb1808.PeriodMode10X
	case "100x":
		b |= usb1808.PeriodMode100X
	case "1000x":
		b |= usb1808.PeriodMode1000X
	default:
		return 0, fmt.Errorf("unknown period multiplier: %s", periodMult)
	}

	switch tickSize {
	case "", "20ns":
		b |= usb1808.TickSize20NS
	case "200ns":
		b |= usb1808.TickSize200NS
	case "2us":
		b |= usb1808.TickSize2000NS
	case "20us":
		b |= usb1808.TickSize20000NS
	default:
		return 0, fmt.Errorf("unknown tick size: %s", tickSize)
	}

	return b, nil
}

// counterOptionsByte parses a comma-separated list of counter options.
func counterOptionsByte(opts string) (uint8, error) {
	if opts == "" {
		return 0, nil
	}
	var b uint8
	for _, opt := range strings.Split(opts, ",") {
		switch strings.TrimSpace(opt) {
		case "clear-on-read":
			b |= usb1808.CounterClearOnRead
		case "no-recycle":
			b |= usb1808.CounterNoRecycle
		case "count-down":
			b |= usb1808.CounterCountDown
		case "range-limit":
			b |= usb1808.CounterRangeLimit
		case "falling-edge":
			b |= usb1808.CounterFallingEdge
		default:
			return 0, fmt.Errorf("unknown counter option: %s", opt)
		}
	}
	return b, nil
}

// encoderOptionsByte builds an encoder options byte from mode and options.
func encoderOptionsByte(mode, opts string) (uint8, error) {
	var b uint8
	switch mode {
	case "", "x1":
		b = usb1808.EncoderX1
	case "x2":
		b = usb1808.EncoderX2
	case "x4":
		b = usb1808.EncoderX4
	default:
		return 0, fmt.Errorf("unknown encoder mode: %s", mode)
	}

	if opts != "" {
		for _, opt := range strings.Split(opts, ",") {
			switch strings.TrimSpace(opt) {
			case "clear-on-z":
				b |= usb1808.EncoderClearOnZ
			case "latch-on-z":
				b |= usb1808.EncoderLatchOnZ
			case "no-recycle":
				b |= usb1808.EncoderNoRecycle
			case "range-limit":
				b |= usb1808.EncoderRangeLimit
			default:
				return 0, fmt.Errorf("unknown encoder option: %s", opt)
			}
		}
	}

	return b, nil
}

// printJSON encodes v as JSON to stdout.
func printJSON(v any) error {
	return json.NewEncoder(os.Stdout).Encode(v)
}

// configureAnalogInputs builds and sends analog input configuration.
// channels is the list of analog channel indices (0-7).
// ranges is the per-channel range for each entry in channels.
// mode is the input mode applied to all channels.
func configureAnalogInputs(dev *usb1808.Device, channels []int, ranges []usb1808.Range, mode usb1808.InputMode) error {
	configs := make([]usb1808.AnalogInChannelConfig, usb1808.NumAInChannels)
	for i := range configs {
		configs[i] = usb1808.AnalogInChannelConfig{
			Channel: i,
			Range:   usb1808.BP10V,
			Mode:    mode,
		}
	}
	for i, ch := range channels {
		if ch < usb1808.NumAInChannels {
			configs[ch].Range = ranges[i]
		}
	}
	return dev.ConfigureAnalogIn(configs)
}
