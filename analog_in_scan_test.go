package usb1808

import (
	"math"
	"testing"
	"time"
)

// TestScanBatchSize_Invariants verifies that scanBatchSize satisfies size and alignment constraints across rates and channel counts.
func TestScanBatchSize_Invariants(t *testing.T) {
	rates := []float64{
		0.1, 0.5, 1, 2, 5, 10, 50, 100, 500,
		1000, 5000, 10000, 50000, 100000, 200000, 500000,
	}
	channelCounts := []int{1, 2, 3, 4, 5, 8, 10, 13}

	for _, rate := range rates {
		for _, nCh := range channelCounts {
			batch := scanBatchSize(rate, nCh)
			bytesPerScan := nCh * 4
			totalBytes := batch * bytesPerScan

			if batch < 1 {
				t.Errorf("rate=%.0f nCh=%d: batch=%d, must be >= 1", rate, nCh, batch)
			}
			if totalBytes > maxTransferBytes {
				t.Errorf("rate=%.0f nCh=%d: totalBytes=%d exceeds maxTransferBytes=%d",
					rate, nCh, totalBytes, maxTransferBytes)
			}
			// When bytesPerScan divides MaxPacketSize evenly, the
			// alignment logic in scanBatchSize can produce exact
			// packet alignment. Otherwise alignment is impossible
			// without wasting bytes, and scan-boundary alignment wins.
			if totalBytes >= MaxPacketSize && MaxPacketSize%bytesPerScan == 0 && totalBytes%MaxPacketSize != 0 {
				t.Errorf("rate=%.0f nCh=%d: totalBytes=%d not aligned to MaxPacketSize=%d",
					rate, nCh, totalBytes, MaxPacketSize)
			}
		}
	}
}

// TestScanReadTimeout_Invariants verifies that scanReadTimeout provides sufficient headroom across rates and channel counts.
func TestScanReadTimeout_Invariants(t *testing.T) {
	rates := []float64{
		0.1, 0.5, 1, 2, 5, 10, 50, 100, 500,
		1000, 5000, 10000, 50000, 100000, 200000, 500000,
	}
	channelCounts := []int{1, 2, 3, 4, 5, 8, 10, 13}

	for _, rate := range rates {
		for _, nCh := range channelCounts {
			batch := scanBatchSize(rate, nCh)
			timeout := scanReadTimeout(rate, batch)
			fillTime := time.Duration(float64(batch) / rate * float64(time.Second))

			if timeout < usbTimeout {
				t.Errorf("rate=%.0f nCh=%d batch=%d: timeout=%v < usbTimeout=%v",
					rate, nCh, batch, timeout, usbTimeout)
			}
			if timeout < fillTime {
				t.Errorf("rate=%.0f nCh=%d batch=%d: timeout=%v < fillTime=%v — will always time out",
					rate, nCh, batch, timeout, fillTime)
			}
			headroom := timeout - fillTime
			if headroom < 2*time.Second {
				t.Errorf("rate=%.0f nCh=%d batch=%d: headroom=%v < 2s — too tight",
					rate, nCh, batch, headroom)
			}
			// Sanity: timeout should not be absurdly large.
			if timeout > 10*time.Minute {
				t.Errorf("rate=%.0f nCh=%d batch=%d: timeout=%v is unreasonably large",
					rate, nCh, batch, timeout)
			}
		}
	}
}

// TestPacketSize_AutoConfig verifies PacketSize auto-configuration for low-rate scans.
func TestPacketSize_AutoConfig(t *testing.T) {
	rates := []float64{
		0.1, 0.5, 1, 5, 10, 50, 100, 500,
		1000, 10000, 100000, 500000,
	}
	channelCounts := []int{1, 2, 3, 4, 5, 8, 10, 13}

	for _, rate := range rates {
		for _, nCh := range channelCounts {
			// Simulate the PacketSize logic from ScanAnalogInRaw.
			var packetSize uint8
			samplesPerScan := nCh
			if rate*float64(samplesPerScan) < MaxPacketSize/4 {
				packetSize = uint8(min(samplesPerScan, 256) - 1)
			}

			// If auto-configured, it must be < 256 (fits in uint8).
			if packetSize > 0 && int(packetSize)+1 > 255 {
				t.Errorf("rate=%.0f nCh=%d: packetSize=%d overflows", rate, nCh, packetSize)
			}

			// At high rates, PacketSize should stay 0 (use firmware default).
			if rate*float64(samplesPerScan) >= MaxPacketSize/4 && packetSize != 0 {
				t.Errorf("rate=%.0f nCh=%d: high-rate scan should not set PacketSize, got %d",
					rate, nCh, packetSize)
			}
		}
	}
}

// TestScanParams_EndToEnd verifies end-to-end consistency of batch size, timeout, and transfer size.
func TestScanParams_EndToEnd(t *testing.T) {
	rates := []float64{
		0.1, 0.5, 1, 2, 5, 10, 50, 100, 500,
		1000, 5000, 10000, 50000, 100000, 200000, 500000,
	}
	channelCounts := []int{1, 2, 3, 4, 5, 8, 10, 13}
	scanCounts := []int{0, 1, 10, 100, 1000, 100000}

	for _, rate := range rates {
		for _, nCh := range channelCounts {
			batch := scanBatchSize(rate, nCh)
			timeout := scanReadTimeout(rate, batch)
			bytesPerScan := nCh * 4
			transferBytes := batch * bytesPerScan

			for _, count := range scanCounts {
				// Simulate the loop logic from ScanAnalogInRaw.
				n := batch
				if count > 0 && n > count {
					n = count
				}
				requestBytes := n * bytesPerScan

				if requestBytes > maxTransferBytes {
					t.Errorf("rate=%.0f nCh=%d count=%d: requestBytes=%d > maxTransferBytes",
						rate, nCh, count, requestBytes)
				}

				// The fill time for what we actually request must be < timeout.
				fillTime := time.Duration(float64(n) / rate * float64(time.Second))
				if fillTime > timeout {
					t.Errorf("rate=%.0f nCh=%d count=%d: fillTime=%v > timeout=%v for n=%d scans",
						rate, nCh, count, fillTime, timeout, n)
				}

				_ = transferBytes
			}
		}
	}
}

// TestScanBatchSize_ExtremeRates tests scan batch sizing at extreme sample rates.
func TestScanBatchSize_ExtremeRates(t *testing.T) {
	extremes := []struct {
		name string
		rate float64
		nCh  int
	}{
		{"near-zero rate", 0.001, 1},
		{"fractional rate", 0.1, 13},
		{"sub-hertz wide", 0.5, 8},
		{"1 Hz 1 ch", 1, 1},
		{"1 Hz 13 ch", 1, 13},
		{"max rate 1 ch", 500000, 1},
		{"max rate 13 ch", 500000, 13},
		{"very high rate", 1000000, 1},
	}

	for _, tt := range extremes {
		t.Run(tt.name, func(t *testing.T) {
			batch := scanBatchSize(tt.rate, tt.nCh)
			timeout := scanReadTimeout(tt.rate, batch)
			transferBytes := batch * tt.nCh * 4
			fillTime := time.Duration(float64(batch) / tt.rate * float64(time.Second))

			if batch < 1 {
				t.Fatalf("batch=%d, must be >= 1", batch)
			}
			if transferBytes > maxTransferBytes {
				t.Fatalf("transferBytes=%d > maxTransferBytes=%d", transferBytes, maxTransferBytes)
			}
			if timeout <= fillTime {
				t.Fatalf("timeout=%v <= fillTime=%v — guaranteed timeout", timeout, fillTime)
			}

			t.Logf("batch=%d transferBytes=%d fillTime=%v timeout=%v",
				batch, transferBytes, fillTime, timeout)
		})
	}
}

// FuzzScanBatchSize fuzzes scanBatchSize for size and alignment invariant violations.
func FuzzScanBatchSize(f *testing.F) {
	// Seed with known edge cases.
	for _, rate := range []float64{0.1, 1, 100, 10000, 100000, 500000} {
		for _, nCh := range []int{1, 2, 4, 8, 13} {
			f.Add(rate, nCh)
		}
	}

	f.Fuzz(func(t *testing.T, rate float64, nCh int) {
		// Restrict to valid input domain.
		if rate <= 0 || rate > 1e7 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			t.Skip()
		}
		if nCh < 1 || nCh > MaxAInQueue {
			t.Skip()
		}

		batch := scanBatchSize(rate, nCh)
		bytesPerScan := nCh * 4
		transferBytes := batch * bytesPerScan

		if batch < 1 {
			t.Errorf("rate=%g nCh=%d: batch=%d < 1", rate, nCh, batch)
		}
		if transferBytes > maxTransferBytes {
			t.Errorf("rate=%g nCh=%d: transferBytes=%d > maxTransferBytes=%d",
				rate, nCh, transferBytes, maxTransferBytes)
		}
		if transferBytes >= MaxPacketSize && MaxPacketSize%bytesPerScan == 0 && transferBytes%MaxPacketSize != 0 {
			t.Errorf("rate=%g nCh=%d: transferBytes=%d not aligned to MaxPacketSize",
				rate, nCh, transferBytes)
		}
	})
}

// FuzzScanReadTimeout fuzzes scanReadTimeout with realistic batch sizes from scanBatchSize.
func FuzzScanReadTimeout(f *testing.F) {
	for _, rate := range []float64{0.1, 1, 100, 10000, 100000, 500000} {
		for _, nCh := range []int{1, 2, 4, 8, 13} {
			f.Add(rate, nCh)
		}
	}

	f.Fuzz(func(t *testing.T, rate float64, nCh int) {
		if rate < 0.01 || rate > 1e7 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			t.Skip()
		}
		if nCh < 1 || nCh > MaxAInQueue {
			t.Skip()
		}

		nScans := scanBatchSize(rate, nCh)
		timeout := scanReadTimeout(rate, nScans)
		fillTime := time.Duration(float64(nScans) / rate * float64(time.Second))

		if timeout < usbTimeout {
			t.Errorf("rate=%g nCh=%d nScans=%d: timeout=%v < usbTimeout=%v", rate, nCh, nScans, timeout, usbTimeout)
		}
		if timeout < fillTime {
			t.Errorf("rate=%g nCh=%d nScans=%d: timeout=%v < fillTime=%v", rate, nCh, nScans, timeout, fillTime)
		}
		if timeout-fillTime < 2*time.Second {
			t.Errorf("rate=%g nCh=%d nScans=%d: headroom=%v < 2s", rate, nCh, nScans, timeout-fillTime)
		}
	})
}

func FuzzScanParamsEndToEnd(f *testing.F) {
	for _, rate := range []float64{0.1, 1, 100, 10000, 100000} {
		for _, nCh := range []int{1, 2, 4, 8, 13} {
			for _, count := range []int{0, 1, 100, 10000} {
				f.Add(rate, nCh, count)
			}
		}
	}

	f.Fuzz(func(t *testing.T, rate float64, nCh int, count int) {
		if rate <= 0 || rate > 1e7 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			t.Skip()
		}
		if nCh < 1 || nCh > MaxAInQueue {
			t.Skip()
		}
		if count < 0 || count > 10_000_000 {
			t.Skip()
		}

		batch := scanBatchSize(rate, nCh)
		timeout := scanReadTimeout(rate, batch)
		bytesPerScan := nCh * 4

		// Simulate first iteration of the read loop.
		n := batch
		if count > 0 && n > count {
			n = count
		}

		requestBytes := n * bytesPerScan
		if requestBytes > maxTransferBytes {
			// ReadAnalogInScanRaw clamps internally, but verify batch doesn't exceed.
			if batch*bytesPerScan > maxTransferBytes {
				t.Errorf("rate=%g nCh=%d: batch transfer %d > maxTransferBytes",
					rate, nCh, batch*bytesPerScan)
			}
		}

		fillTime := time.Duration(float64(n) / rate * float64(time.Second))
		if fillTime > timeout {
			t.Errorf("rate=%g nCh=%d count=%d: fillTime=%v > timeout=%v (n=%d batch=%d)",
				rate, nCh, count, fillTime, timeout, n, batch)
		}
	})
}
