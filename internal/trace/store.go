package trace

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/pluginapi"
)

type Event struct {
	Seq        int64              `json:"seq"`
	At         string             `json:"at"`
	Kind       string             `json:"kind"`
	Request    *pluginapi.Request `json:"request,omitempty"`
	Payload    json.RawMessage    `json:"payload,omitempty"`
	Error      *pluginapi.Failure `json:"error,omitempty"`
	DurationMS int64              `json:"duration_ms,omitempty"`
}
type Store struct {
	mu    sync.Mutex
	dir   string
	limit int64
	seq   map[string]int64
}

func Open(root string, limit int64) (*Store, error) {
	if limit <= 0 {
		limit = 64 << 20
	}
	dir := filepath.Join(root, ".x-mock", "traces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, limit: limit, seq: map[string]int64{}}, nil
}
func (s *Store) Append(run string, e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scenario.ValidID(run) {
		return e, pluginapi.Invalid("invalid run ID")
	}
	path := filepath.Join(s.dir, run+".ndjson")
	if _, ok := s.seq[run]; !ok {
		events, err := s.read(run, 0, 0)
		if err != nil {
			return e, err
		}
		if len(events) > 0 {
			s.seq[run] = events[len(events)-1].Seq
		}
	}
	e.Seq = s.seq[run] + 1
	e.At = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	raw = append(raw, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return e, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return e, err
	}
	if stat.Size()+int64(len(raw)) > s.limit {
		return e, pluginapi.Fail("TRACE_FULL", "run trace exceeded size limit")
	}
	if _, err = file.Write(raw); err != nil {
		return e, err
	}
	s.seq[run] = e.Seq
	return e, nil
}
func (s *Store) Read(run string, after int64, limit int) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 || limit > 200 {
		return nil, pluginapi.Invalid("trace limit must be 1..200")
	}
	return s.read(run, after, limit)
}
func (s *Store) read(run string, after int64, limit int) ([]Event, error) {
	if !scenario.ValidID(run) {
		return nil, pluginapi.Invalid("invalid run ID")
	}
	out := []Event{}
	file, err := os.Open(filepath.Join(s.dir, run+".ndjson"))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 4096), 4<<20)
	for scan.Scan() {
		var e Event
		if err = pluginapi.Decode(scan.Bytes(), &e); err != nil {
			return nil, err
		}
		if e.Seq > after {
			out = append(out, e)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, scan.Err()
}
