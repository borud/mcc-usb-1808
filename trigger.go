package usb1808

// TriggerConfig reads the trigger configuration byte.
func (d *Device) TriggerConfig() (uint8, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdTriggerConfig, 0, 0, 1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

// SetTriggerConfig writes the trigger configuration byte.
// Bit 0: mode (0=level, 1=edge). Bit 1: polarity (0=low/falling, 1=high/rising).
func (d *Device) SetTriggerConfig(config uint8) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdTriggerConfig, 0, 0, []byte{config})
}

// PatternDetectConfig holds pattern detection trigger configuration.
type PatternDetectConfig struct {
	Value   uint8 // Pattern to match (DIO pin values).
	Mask    uint8 // Bits to compare.
	Options uint8 // Comparison mode (bits 1-2).
}

// PatternDetect reads the pattern detection trigger configuration.
func (d *Device) PatternDetect() (PatternDetectConfig, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	data, err := d.transport.ControlIn(cmdPatternDetect, 0, 0, 3)
	if err != nil {
		return PatternDetectConfig{}, err
	}
	return PatternDetectConfig{
		Value:   data[0],
		Mask:    data[1],
		Options: data[2],
	}, nil
}

// SetPatternDetect writes the pattern detection trigger configuration.
func (d *Device) SetPatternDetect(cfg PatternDetectConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.transport.ControlOut(cmdPatternDetect, 0, 0, []byte{cfg.Value, cfg.Mask, cfg.Options})
}
