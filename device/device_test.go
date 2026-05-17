package device

import (
	"bytes"
	"testing"

	"github.com/borud/mcc-usb-1808/v4/transport"
	"github.com/borud/mcc-usb-1808/v4/wire"
)

func TestBlinkLED(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.BlinkLED(3); err != nil {
		t.Fatal(err)
	}

	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.Calls))
	}
	call := mock.Calls[0]
	if call.Method != "ControlOut" {
		t.Errorf("method = %s, want ControlOut", call.Method)
	}
	if call.Request != cmdBlinkLED {
		t.Errorf("request = 0x%02x, want 0x%02x", call.Request, cmdBlinkLED)
	}
	if !bytes.Equal(call.Data, []byte{3}) {
		t.Errorf("data = %x, want [03]", call.Data)
	}
}

func TestStatus(t *testing.T) {
	statusWord := wire.PutUint16LE(statusFPGAConfigured | statusAInScanRunning)
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: statusWord},
	)
	dev := NewDevice(mock, USB1808)

	st, err := dev.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.FPGAConfigured() {
		t.Error("expected FPGAConfigured")
	}
	if !st.AInScanRunning() {
		t.Error("expected AInScanRunning")
	}
	if st.AOutScanRunning() {
		t.Error("did not expect AOutScanRunning")
	}
}

func TestFPGAVersion(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: wire.PutUint16LE(0x0102)},
	)
	dev := NewDevice(mock, USB1808)

	major, minor, err := dev.FPGAVersion()
	if err != nil {
		t.Fatal(err)
	}
	if major != 1 || minor != 2 {
		t.Errorf("version = %d.%d, want 1.2", major, minor)
	}
}

func TestSerialNumber(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{Data: []byte("12345678")},
	)
	dev := NewDevice(mock, USB1808)

	sn, err := dev.SerialNumber()
	if err != nil {
		t.Fatal(err)
	}
	if sn != "12345678" {
		t.Errorf("serial = %q, want %q", sn, "12345678")
	}
}

func TestReset(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{},
	)
	dev := NewDevice(mock, USB1808)

	if err := dev.Reset(); err != nil {
		t.Fatal(err)
	}
	if mock.Calls[0].Request != cmdReset {
		t.Errorf("request = 0x%02x, want 0x%02x", mock.Calls[0].Request, cmdReset)
	}
}

func TestClose(t *testing.T) {
	mock := transport.NewMockTransport(
		transport.MockResponse{}, // AInScanStop
		transport.MockResponse{}, // AOutScanStop
	)
	dev := NewDevice(mock, USB1808)
	dev.initialized.Store(true)

	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}
	if !mock.Closed() {
		t.Error("expected transport to be closed")
	}
}

func TestModelString(t *testing.T) {
	if USB1808.String() != "USB-1808" {
		t.Errorf("USB1808.String() = %s", USB1808.String())
	}
	if USB1808X.String() != "USB-1808X" {
		t.Errorf("USB1808X.String() = %s", USB1808X.String())
	}
}
