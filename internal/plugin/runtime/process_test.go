package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestProcessRejectsUninstalledPlugin(t *testing.T) {
	c, err := catalog.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	factory := New(c)
	if _, err = factory.NewRight(context.Background(), pluginapi.Reference{ID: "absent", Role: pluginapi.Right, Version: "0.1.0"}); pluginapi.Code(err) != "PLUGIN_NOT_INSTALLED" {
		t.Fatal(err)
	}
}
func TestProcessOwnsChildAndClosesOnHandshakeFailure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad-plugin")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nprintf 'invalid-json\\n'\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	released := false
	m := pluginapi.Manifest{Descriptor: pluginapi.Descriptor{Ref: pluginapi.Reference{ID: "bad", Role: pluginapi.Right, Version: "0.1.0"}, APIMajor: 1, Contracts: []pluginapi.Contract{{ID: "echo", Version: 1}}, ConfigSchema: json.RawMessage(`{"type":"object"}`)}}
	process, err := launch(ctx, p, m, func() { released = true }, nil)
	if err == nil {
		process.Close()
		t.Fatal("invalid handshake accepted")
	}
	if !released {
		t.Fatal("failed process retained catalog reference")
	}
}
