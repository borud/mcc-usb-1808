package device

import (
	"math"
	"testing"
)

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
			// Packet alignment must hold for ALL channel counts. The
			// device delivers data in MaxPacketSize chunks; a buffer
			// whose size isn't a multiple of MaxPacketSize lets a
			// full final packet spill past the end of the transfer
			// and fire LIBUSB_TRANSFER_OVERFLOW. The previous
			// guard ("only check when MaxPacketSize divides
			// bytesPerScan") was wrong because the actual USB
			// requirement is MaxPacketSize alignment, not the
			// other direction.
			if size >= MaxPacketSize && size%MaxPacketSize != 0 {
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
		if size >= MaxPacketSize && size%MaxPacketSize != 0 {
			t.Errorf("rate=%g nCh=%d: size=%d not aligned to MaxPacketSize",
				rate, nCh, size)
		}
		if size%bytesPerScan != 0 {
			t.Errorf("rate=%g nCh=%d: size=%d not aligned to bytesPerScan=%d",
				rate, nCh, size, bytesPerScan)
		}
	})
}
