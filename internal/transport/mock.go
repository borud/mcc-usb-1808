package transport

import (
	"fmt"
	"time"
)

// Call records a single transport method invocation.
type Call struct {
	Method   string
	Request  uint8
	WValue   uint16
	WIndex   uint16
	Data     []byte
	Endpoint uint8
	Length   int
}

// MockResponse is a canned response for a transport call.
type MockResponse struct {
	Data []byte
	Err  error
}

// MockTransport records all transport calls and replays canned responses.
// It implements Transport for testing without hardware.
type MockTransport struct {
	Calls     []Call
	Responses []MockResponse
	nextResp  int
	closed    bool
}

// NewMockTransport creates a MockTransport with the given canned responses.
// Responses are consumed in order.
func NewMockTransport(responses ...MockResponse) *MockTransport {
	return &MockTransport{Responses: responses}
}

func (m *MockTransport) nextResponse() ([]byte, error) {
	if m.nextResp >= len(m.Responses) {
		return nil, fmt.Errorf("mock: no more canned responses (call #%d)", m.nextResp)
	}
	r := m.Responses[m.nextResp]
	m.nextResp++
	return r.Data, r.Err
}

// ControlOut implements Transport.
func (m *MockTransport) ControlOut(request uint8, wValue, wIndex uint16, data []byte) error {
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.Calls = append(m.Calls, Call{
		Method:  "ControlOut",
		Request: request,
		WValue:  wValue,
		WIndex:  wIndex,
		Data:    dataCopy,
	})
	_, err := m.nextResponse()
	return err
}

// ControlIn implements Transport.
func (m *MockTransport) ControlIn(request uint8, wValue, wIndex uint16, length int) ([]byte, error) {
	m.Calls = append(m.Calls, Call{
		Method:  "ControlIn",
		Request: request,
		WValue:  wValue,
		WIndex:  wIndex,
		Length:  length,
	})
	return m.nextResponse()
}

// BulkRead implements Transport.
func (m *MockTransport) BulkRead(endpoint uint8, length int, _ time.Duration) ([]byte, error) {
	m.Calls = append(m.Calls, Call{
		Method:   "BulkRead",
		Endpoint: endpoint,
		Length:   length,
	})
	return m.nextResponse()
}

// BulkReadInto implements Transport.
func (m *MockTransport) BulkReadInto(endpoint uint8, buf []byte, _ time.Duration) (int, error) {
	m.Calls = append(m.Calls, Call{
		Method:   "BulkReadInto",
		Endpoint: endpoint,
		Length:   len(buf),
	})
	data, err := m.nextResponse()
	if err != nil {
		return 0, err
	}
	n := copy(buf, data)
	return n, nil
}

// BulkWrite implements Transport.
func (m *MockTransport) BulkWrite(endpoint uint8, data []byte, _ time.Duration) (int, error) {
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.Calls = append(m.Calls, Call{
		Method:   "BulkWrite",
		Endpoint: endpoint,
		Data:     dataCopy,
	})
	resp, err := m.nextResponse()
	if err != nil {
		return 0, err
	}
	_ = resp
	return len(data), nil
}

// Close implements Transport.
func (m *MockTransport) Close() error {
	m.closed = true
	return nil
}

// Closed returns true if Close was called.
func (m *MockTransport) Closed() bool {
	return m.closed
}
