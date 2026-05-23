package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// OverrunEvent records a single FIFO overrun during capture.
type OverrunEvent struct {
	FrameIndex          uint64  `json:"frame_index"`
	Timestamp           int64   `json:"timestamp"`
	EstimatedLostFrames uint64  `json:"estimated_lost_frames,omitempty"`
	RecoveryDurationMs  float64 `json:"recovery_duration_ms,omitempty"`
}

// OverrunLog writes overrun events to a sidecar JSONL file during capture.
// The sidecar serves as crash recovery: if the process dies before closing
// segments, the log preserves overrun history. Thread-safe.
type OverrunLog struct {
	f      *os.File
	events []OverrunEvent
	mu     sync.Mutex
}

// NewOverrunLog creates or opens the overrun sidecar file in dir.
// The file is created lazily on the first Record call.
func NewOverrunLog(dir string) (*OverrunLog, error) {
	path := filepath.Join(dir, "overruns.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) // #nosec G304
	if err != nil {
		return nil, err
	}
	return &OverrunLog{f: f}, nil
}

// Record appends an overrun event to the sidecar file and in-memory list.
func (ol *OverrunLog) Record(e OverrunEvent) error {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	buf, err := json.Marshal(e)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	if _, err := ol.f.Write(buf); err != nil {
		return err
	}
	if err := ol.f.Sync(); err != nil {
		return err
	}
	ol.events = append(ol.events, e)
	return nil
}

// Events returns a copy of all recorded overrun events.
func (ol *OverrunLog) Events() []OverrunEvent {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	return append([]OverrunEvent(nil), ol.events...)
}

// Count returns the number of recorded overrun events.
func (ol *OverrunLog) Count() int {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	return len(ol.events)
}

// Close closes the sidecar file and removes it if no events were recorded.
func (ol *OverrunLog) Close() error {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	name := ol.f.Name()
	if err := ol.f.Close(); err != nil {
		return err
	}
	if len(ol.events) == 0 {
		os.Remove(name)
	}
	return nil
}
