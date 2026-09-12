package scenario

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"xmock.local/x-mock-mcp/pluginapi"
)

type Document struct {
	ID              string          `json:"id"`
	Version         int64           `json:"version"`
	PluginID        string          `json:"plugin_id"`
	PluginVersion   string          `json:"plugin_version"`
	ContractID      string          `json:"contract_id"`
	ContractVersion int             `json:"contract_version"`
	Input           json.RawMessage `json:"input,omitempty"`
	Body            json.RawMessage `json:"body"`
}
type Snapshot struct {
	Document      Document          `json:"document"`
	SHA256        string            `json:"sha256"`
	PluginDigests map[string]string `json:"plugin_digests"`
}
type Validator func(context.Context, Document) (Document, error)
type Store struct {
	ProjectRoot string
	dir         string
	mu          sync.Mutex
	validate    Validator
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}$`)

func ValidID(s string) bool { return validID.MatchString(s) }
func Open(root string, validate Validator) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(abs, ".x-mock", "scenarios")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if validate == nil {
		return nil, pluginapi.Invalid("scenario validator required")
	}
	return &Store{ProjectRoot: abs, dir: dir, validate: validate}, nil
}
func (s *Store) locked(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(filepath.Join(s.dir, "store.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}
func (s *Store) versions(id string) ([]Document, error) {
	if !ValidID(id) {
		return nil, pluginapi.Invalid("invalid scenario ID")
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return []Document{}, nil
	}
	if err != nil {
		return nil, err
	}
	var docs []Document
	err = pluginapi.Decode(data, &docs)
	return docs, err
}
func (s *Store) Put(ctx context.Context, d Document, expected int64) (out Document, err error) {
	if !ValidID(d.ID) || expected < 0 || (d.Version != 0 && d.Version != expected+1) || len(d.Body) == 0 || len(d.Body)+len(d.Input) > 2<<20 {
		return out, pluginapi.Invalid("invalid document ID, version or size")
	}
	ref := pluginapi.Reference{ID: d.PluginID, Version: d.PluginVersion, Role: pluginapi.Right}
	if err = ref.Validate(); err != nil {
		return out, err
	}
	if d.ContractID == "" || d.ContractVersion < 1 {
		return out, pluginapi.Invalid("contract required")
	}
	validated, err := s.validate(ctx, d)
	if err != nil {
		return out, err
	}
	validated.Version = expected + 1
	err = s.locked(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		docs, err := s.versions(d.ID)
		if err != nil {
			return err
		}
		if int64(len(docs)) != expected {
			return pluginapi.Fail("STATE_CONFLICT", "scenario version changed")
		}
		docs = append(docs, validated)
		raw, err := json.Marshal(docs)
		if err != nil {
			return err
		}
		temp, err := os.CreateTemp(s.dir, "scenario-*.tmp")
		if err != nil {
			return err
		}
		defer os.Remove(temp.Name())
		if _, err = temp.Write(raw); err != nil {
			temp.Close()
			return err
		}
		if err = temp.Sync(); err != nil {
			temp.Close()
			return err
		}
		if err = temp.Close(); err != nil {
			return err
		}
		return os.Rename(temp.Name(), filepath.Join(s.dir, d.ID+".json"))
	})
	if err == nil {
		out = validated
	}
	return
}
func (s *Store) Get(ctx context.Context, id string, version int64) (out Document, err error) {
	err = s.locked(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		docs, err := s.versions(id)
		if err != nil {
			return err
		}
		if version < 1 || version > int64(len(docs)) {
			return pluginapi.Fail("NOT_FOUND", "scenario version does not exist")
		}
		out = docs[version-1]
		return nil
	})
	return
}
func (s *Store) Freeze(ctx context.Context, id string, version int64, digests map[string]string) (Snapshot, error) {
	d, err := s.Get(ctx, id, version)
	if err != nil {
		return Snapshot{}, err
	}
	checked, err := s.validate(ctx, d)
	if err != nil {
		return Snapshot{}, err
	}
	a, _ := json.Marshal(d.Body)
	b, _ := json.Marshal(checked.Body)
	if string(a) != string(b) {
		return Snapshot{}, pluginapi.Fail("STATE_CONFLICT", "stored scenario is no longer canonical")
	}
	snapshot := Snapshot{Document: d, PluginDigests: map[string]string{}}
	for k, v := range digests {
		snapshot.PluginDigests[k] = v
	}
	raw, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	snapshot.SHA256 = hex.EncodeToString(sum[:])
	return snapshot, nil
}
