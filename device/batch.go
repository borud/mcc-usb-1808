package device

import "time"

// scanBatchSize computes the number of scans to read per USB bulk transfer.
// Targets ~100ms worth of data per read, capped at maxTransferBytes, aligned
// to MaxPacketSize (512) boundaries.
func scanBatchSize(rate float64, nChannels int) int {
	n := int(rate * 0.1) // 100ms of data
	if n < 1 {
		n = 1
	}
	bytesPerScan := nChannels * 4
	totalBytes := n * bytesPerScan
	if totalBytes > maxTransferBytes {
		totalBytes = maxTransferBytes
	}
	if totalBytes >= MaxPacketSize {
		totalBytes = (totalBytes / MaxPacketSize) * MaxPacketSize
	}
	return totalBytes / bytesPerScan
}

// scanReadTimeout computes the USB bulk read timeout for a given batch.
func scanReadTimeout(rate float64, nScans int) time.Duration {
	fillTime := time.Duration(float64(nScans) / rate * float64(time.Second))
	timeout := fillTime + 2*time.Second
	if timeout < usbTimeout {
		timeout = usbTimeout
	}
	return timeout
}

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
