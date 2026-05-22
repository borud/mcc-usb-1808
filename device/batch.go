package device

// ringStageSize computes the buffer size in bytes for each async ring transfer.
// Targets ~10ms of data, aligned to MaxPacketSize, capped at ringMaxStageSize.
func ringStageSize(rate float64, nChannels int) int {
	n := int(rate * 0.01) // 10ms of data (in scans)
	if n < 1 {
		n = 1
	}
	bytesPerScan := nChannels * 4
	totalBytes := n * bytesPerScan
	if totalBytes > ringMaxStageSize {
		totalBytes = ringMaxStageSize
	}
	if totalBytes >= MaxPacketSize {
		totalBytes = (totalBytes / MaxPacketSize) * MaxPacketSize
	}
	if totalBytes < MaxPacketSize {
		totalBytes = MaxPacketSize
	}
	totalBytes = (totalBytes / bytesPerScan) * bytesPerScan
	if totalBytes < bytesPerScan {
		totalBytes = bytesPerScan
	}
	return totalBytes
}
