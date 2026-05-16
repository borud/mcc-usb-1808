package usb1808

import (
	"encoding/binary"
	"fmt"

	"github.com/borud/mcc-usb-1808/v3/internal/wire"
)

func validateTimer(timer int) error {
	if timer < 0 || timer >= NumTimers {
		return fmt.Errorf("%w: %d", ErrInvalidTimer, timer)
	}
	return nil
}

// TimerControl reads the timer control register.
func (d *Device) TimerControl(timer int) (uint8, error) {
	if err := validateTimer(timer); err != nil {
		return 0, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdTimerControl, 0, uint16(timer), 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// SetTimerControl writes the timer control register.
func (d *Device) SetTimerControl(timer int, control uint8) error {
	if err := validateTimer(timer); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdTimerControl, uint16(control), uint16(timer), nil)
}

// TimerConfig holds timer configuration parameters.
type TimerConfig struct {
	Frequency float64 // Output frequency in Hz.
	DutyCycle float64 // Duty cycle 0.0 to 1.0.
	Count     uint32  // Number of pulses (0 = continuous).
	Delay     float64 // Initial delay in seconds.
}

// SetTimerParams writes timer parameters (frequency, duty cycle, count, delay).
// Parameters are cached locally because firmware returns incorrect values on read.
func (d *Device) SetTimerParams(timer int, cfg TimerConfig) error {
	if err := validateTimer(timer); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	period, pulseWidth := wire.TimerParams(cfg.Frequency, cfg.DutyCycle)
	delay := uint32(cfg.Delay * float64(BaseClock))

	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], period)
	binary.LittleEndian.PutUint32(buf[4:8], pulseWidth)
	binary.LittleEndian.PutUint32(buf[8:12], cfg.Count)
	binary.LittleEndian.PutUint32(buf[12:16], delay)

	if err := d.transport.ControlOut(cmdTimerParams, 0, uint16(timer), buf); err != nil {
		return err
	}

	// Cache locally (firmware returns wrong values on read).
	d.timerCache[timer] = cfg
	return nil
}

// TimerParams returns the cached timer parameters.
// The firmware returns incorrect values for TIMER_PARAMETERS read,
// so this returns the last written values.
func (d *Device) TimerParams(timer int) (TimerConfig, error) {
	if err := validateTimer(timer); err != nil {
		return TimerConfig{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.timerCache[timer], nil
}

// StartTimer configures and enables the specified timer.
func (d *Device) StartTimer(timer int, cfg TimerConfig) error {
	if err := d.SetTimerParams(timer, cfg); err != nil {
		return err
	}
	return d.SetTimerControl(timer, TimerEnable)
}

// StopTimer disables the specified timer.
func (d *Device) StopTimer(timer int) error {
	return d.SetTimerControl(timer, 0)
}
