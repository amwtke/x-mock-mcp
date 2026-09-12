package scenario

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSnapshotAndVersionConflict(t *testing.T) {
	s, err := Open(t.TempDir(), func(_ context.Context, d Document) (Document, error) { return d, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	d := Document{ID: "shop", PluginID: "right-test", PluginVersion: "0.1.0", ContractID: "test", ContractVersion: 1, Body: json.RawMessage(`{"value":1}`)}
	d, err = s.Put(ctx, d, 0)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Freeze(ctx, "shop", 1, map[string]string{"right": "digest"})
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next := d
			next.Version = 0
			next.Body = json.RawMessage(`{"value":2}`)
			if _, err := s.Put(ctx, next, 1); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("lost version CAS", successes.Load())
	}
	if string(snap.Document.Body) != `{"value":1}` {
		t.Fatal("snapshot mutated")
	}
	old, err := s.Get(ctx, "shop", 1)
	if err != nil || string(old.Body) != `{"value":1}` {
		t.Fatal(old, err)
	}
	reopened, err := Open(s.ProjectRoot, func(_ context.Context, d Document) (Document, error) { return d, nil })
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, "shop", 2)
	if err != nil || got.Version != 2 {
		t.Fatal(got, err)
	}
}
func TestStoreDoesNotBypassPreparation(t *testing.T) {
	s, _ := Open(t.TempDir(), func(context.Context, Document) (Document, error) { return Document{}, context.Canceled })
	if _, err := s.Put(context.Background(), Document{ID: "x", PluginID: "x", PluginVersion: "0.1.0", ContractID: "x", ContractVersion: 1, Body: json.RawMessage(`{}`)}, 0); err == nil {
		t.Fatal("unchecked candidate saved")
	}
}
