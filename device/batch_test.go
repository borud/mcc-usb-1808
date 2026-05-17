package device

import (
	"math"
	"testing"
	"time"
)

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
			if totalBytes >= MaxPacketSize && MaxPacketSize%bytesPerScan == 0 && totalBytes%MaxPacketSize != 0 {
				t.Errorf("rate=%.0f nCh=%d: totalBytes=%d not aligned to MaxPacketSize=%d",
					rate, nCh, totalBytes, MaxPacketSize)
			}
		}
	}
}

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
				t.Errorf("rate=%.0f nCh=%d batch=%d: timeout=%v < fillTime=%v",
					rate, nCh, batch, timeout, fillTime)
			}
			headroom := timeout - fillTime
			if headroom < 2*time.Second {
				t.Errorf("rate=%.0f nCh=%d batch=%d: headroom=%v < 2s",
					rate, nCh, batch, headroom)
			}
			if timeout > 10*time.Minute {
				t.Errorf("rate=%.0f nCh=%d batch=%d: timeout=%v is unreasonably large",
					rate, nCh, batch, timeout)
			}
		}
	}
}

func TestRingStageSize_Invariants(t *testing.T) {
	rates := []float64{
		0.1, 0.5, 1, 2, 5, 10, 50, 100, 500,
		1000, 5000, 10000, 50000, 100000, 200000, 500000,
	}
	channelCounts := []int{1, 2, 3, 4, 5, 8, 10, 13}

	for _, rate := range rates {
		for _, nCh := range channelCounts {
			size := ringStageSize(rate, nCh)
			bytesPerScan := nCh * 4

			if size < bytesPerScan {
				t.Errorf("rate=%.0f nCh=%d: size=%d < bytesPerScan=%d",
					rate, nCh, size, bytesPerScan)
			}
			if size > ringMaxStageSize {
				t.Errorf("rate=%.0f nCh=%d: size=%d > ringMaxStageSize=%d",
					rate, nCh, size, ringMaxStageSize)
			}
			if size >= MaxPacketSize && MaxPacketSize%bytesPerScan == 0 && size%MaxPacketSize != 0 {
				t.Errorf("rate=%.0f nCh=%d: size=%d not aligned to MaxPacketSize=%d",
					rate, nCh, size, MaxPacketSize)
			}
			if size%bytesPerScan != 0 {
				t.Errorf("rate=%.0f nCh=%d: size=%d not aligned to bytesPerScan=%d",
					rate, nCh, size, bytesPerScan)
			}
		}
	}
}

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
				t.Fatalf("timeout=%v <= fillTime=%v", timeout, fillTime)
			}
		})
	}
}

func FuzzScanBatchSize(f *testing.F) {
	for _, rate := range []float64{0.1, 1, 100, 10000, 100000, 500000} {
		for _, nCh := range []int{1, 2, 4, 8, 13} {
			f.Add(rate, nCh)
		}
	}

	f.Fuzz(func(t *testing.T, rate float64, nCh int) {
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

func FuzzRingStageSize(f *testing.F) {
	for _, rate := range []float64{0.1, 1, 100, 10000, 100000, 500000} {
		for _, nCh := range []int{1, 2, 4, 8, 13} {
			f.Add(rate, nCh)
		}
	}

	f.Fuzz(func(t *testing.T, rate float64, nCh int) {
		if rate <= 0 || rate > 1e7 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			t.Skip()
		}
		if nCh < 1 || nCh > MaxAInQueue {
			t.Skip()
		}

		size := ringStageSize(rate, nCh)
		bytesPerScan := nCh * 4

		if size < bytesPerScan {
			t.Errorf("rate=%g nCh=%d: size=%d < bytesPerScan=%d",
				rate, nCh, size, bytesPerScan)
		}
		if size > ringMaxStageSize {
			t.Errorf("rate=%g nCh=%d: size=%d > ringMaxStageSize=%d",
				rate, nCh, size, ringMaxStageSize)
		}
		if size >= MaxPacketSize && MaxPacketSize%bytesPerScan == 0 && size%MaxPacketSize != 0 {
			t.Errorf("rate=%g nCh=%d: size=%d not aligned to MaxPacketSize",
				rate, nCh, size)
		}
		if size%bytesPerScan != 0 {
			t.Errorf("rate=%g nCh=%d: size=%d not aligned to bytesPerScan=%d",
				rate, nCh, size, bytesPerScan)
		}
	})
}
