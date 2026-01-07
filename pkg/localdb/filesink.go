package localdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"vanguardedge/pkg/model"
)

// FileEventSink appends shutdown events to a JSONL file with fsync for durability.
type FileEventSink struct {
	path string
	mu   sync.Mutex
}

// NewFileEventSink creates a file-backed event sink at the given path.
func NewFileEventSink(path string) *FileEventSink {
	return &FileEventSink{path: path}
}

// Persist writes the shutdown event as a single JSON line and fsyncs it.
func (s *FileEventSink) Persist(event model.ShutdownEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err := f.Write(append(payload, '\n')); err != nil {
		return err
	}

	return f.Sync()
}
