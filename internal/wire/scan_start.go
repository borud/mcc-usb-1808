package wire

// AInScanStartPayload builds the 14-byte payload for the AIN_SCAN_START command.
func AInScanStartPayload(scanCount, retrigCount, pacerPeriod uint32, packetSize, options uint8) []byte {
	buf := make([]byte, 14)
	copy(buf[0:4], PutUint32LE(scanCount))
	copy(buf[4:8], PutUint32LE(retrigCount))
	copy(buf[8:12], PutUint32LE(pacerPeriod))
	buf[12] = packetSize
	buf[13] = options
	return buf
}

// AOutScanStartPayload builds the 13-byte payload for the AOUT_SCAN_START command.
// Note: AOut lacks the packet_size field present in AIn.
func AOutScanStartPayload(scanCount, retrigCount, pacerPeriod uint32, options uint8) []byte {
	buf := make([]byte, 13)
	copy(buf[0:4], PutUint32LE(scanCount))
	copy(buf[4:8], PutUint32LE(retrigCount))
	copy(buf[8:12], PutUint32LE(pacerPeriod))
	buf[12] = options
	return buf
}

// PacerPeriod calculates the pacer period from a frequency in Hz.
// Returns 0 for external clock (frequency <= 0).
func PacerPeriod(frequency float64) uint32 {
	if frequency <= 0 {
		return 0
	}
	return uint32(100_000_000/frequency) - 1
}

// TimerParams encodes timer frequency and duty cycle into period and pulse width
// values for the TIMER_PARAMETERS command.
// Returns (period, pulseWidth).
func TimerParams(frequency, dutyCycle float64) (uint32, uint32) {
	period := uint32(100_000_000/frequency) - 1
	pulseWidth := uint32(float64(period+1)*dutyCycle) - 1
	return period, pulseWidth
}
