package device

import (
	"testing"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

func TestCreateScan_ConfigSendsCorrectTransfers(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // ADC setup
		transport.MockResponse{}, // scan queue config
	)
	dev := NewDevice(mock, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP5V, Mode: SingleEnded},
			{Index: ScanChanDIO, Type: ChannelTypeDIO},
		},
		Rate: 100000,
	}
	h, err := dev.CreateScan(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify ADC setup was sent.
	adcCall := mock.Calls[0]
	if adcCall.Request != cmdADCSetup {
		t.Errorf("first call request = 0x%02x, want 0x%02x", adcCall.Request, cmdADCSetup)
	}
	// Ch0: BP10V|Diff = 0x00, Ch1: BP5V|(SE<<2) = 0x05
	if adcCall.Data[0] != 0x00 {
		t.Errorf("ADC ch0 = 0x%02x, want 0x00", adcCall.Data[0])
	}
	if adcCall.Data[1] != 0x05 {
		t.Errorf("ADC ch1 = 0x%02x, want 0x05", adcCall.Data[1])
	}

	// Verify scan queue config.
	queueCall := mock.Calls[1]
	if queueCall.Request != cmdAInConfig {
		t.Errorf("second call request = 0x%02x, want 0x%02x", queueCall.Request, cmdAInConfig)
	}
	if queueCall.WIndex != 2 { // lastChan - 1 = 3 channels - 1 = 2
		t.Errorf("wIndex = %d, want 2", queueCall.WIndex)
	}
	if queueCall.Data[0] != 0 || queueCall.Data[1] != 1 || queueCall.Data[2] != ScanChanDIO {
		t.Errorf("queue = %v, want [0 1 8]", queueCall.Data[:3])
	}

	// Verify handle properties.
	if h.Rate() != 100000 {
		t.Errorf("rate = %d, want 100000", h.Rate())
	}
	if h.FrameSize() != 3*4 {
		t.Errorf("frameSize = %d, want %d", h.FrameSize(), 3*4)
	}
	if len(h.Channels()) != 3 {
		t.Errorf("channels = %d, want 3", len(h.Channels()))
	}
}

func TestCreateScan_ValidationErrors(t *testing.T) {
	mock := transport.NewMockTransport()
	dev := NewDevice(mock, USB1808)

	// Empty channels.
	_, err := dev.CreateScan(ScanConfig{Channels: nil, Rate: 1000})
	if err == nil {
		t.Error("expected error for empty channels")
	}

	// Invalid rate.
	_, err = dev.CreateScan(ScanConfig{
		Channels: []ChannelConfig{{Index: 0, Type: ChannelTypeAnalog}},
		Rate:     0,
	})
	if err == nil {
		t.Error("expected error for zero rate")
	}

	// Invalid channel index.
	_, err = dev.CreateScan(ScanConfig{
		Channels: []ChannelConfig{{Index: 99, Type: ChannelTypeAnalog}},
		Rate:     1000,
	})
	if err == nil {
		t.Error("expected error for invalid channel index")
	}
}

func TestScanHandle_StartAndStop(t *testing.T) {
	// Build mock that handles: ADC setup, queue config, clear FIFO, scan start,
	// then bulk reads that return data, then stop/flush commands.
	nCh := 2
	frameSize := nCh * 4
	// Create one frame of data.
	frameData := make([]byte, frameSize)
	copy(frameData[0:4], wire.PutUint32LE(131072)) // midscale
	copy(frameData[4:8], wire.PutUint32LE(262143)) // max

	mock := transport.NewMockTransport(
		transport.MockResponse{},              // ADC setup
		transport.MockResponse{},              // scan queue config
		transport.MockResponse{},              // clear FIFO
		transport.MockResponse{},              // scan start
		transport.MockResponse{Data: frameData}, // first bulk read
		transport.MockResponse{Data: frameData}, // second bulk read
		transport.MockResponse{Data: frameData}, // third bulk read
		transport.MockResponse{Data: frameData}, // fourth bulk read — enough for readers
		// Stop will trigger errors on subsequent reads, causing readers to exit.
	)
	dev := NewDevice(mock, USB1808)

	cfg := ScanConfig{
		Channels: []ChannelConfig{
			{Index: 0, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
			{Index: 1, Type: ChannelTypeAnalog, Range: BP10V, Mode: Differential},
		},
		Rate:  1000,
		Count: 2,
	}
	h, err := dev.CreateScan(cfg, WithPipelineDepth(4), WithConcurrentReaders(1))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Start(); err != nil {
		t.Fatal(err)
	}

	// Read at least one chunk.
	chunk, ok := <-h.Chunks()
	if !ok {
		t.Fatal("chunks channel closed without data")
	}
	if len(chunk) < frameSize {
		t.Errorf("chunk size = %d, want >= %d", len(chunk), frameSize)
	}

	// Channel should close when count is reached or we stop.
	// Drain remaining.
	for range h.Chunks() {
	}

	if h.Err() != nil {
		t.Errorf("unexpected error: %v", h.Err())
	}
}
