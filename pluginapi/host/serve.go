package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"xmock.local/x-mock-mcp/internal/plugin/ipc"
	"xmock.local/x-mock-mcp/pluginapi"
)

// ServeLeft and ServeRight expose the versioned private Plugin API. They are
// independent of the MCP transport used by coding agents.
func ServeLeft(left pluginapi.LeftStrategy) error    { return serve(left, nil) }
func ServeRight(right pluginapi.RightStrategy) error { return serve(nil, right) }

type endpoint struct {
	left      pluginapi.LeftStrategy
	right     pluginapi.RightStrategy
	peer      *ipc.Peer
	ready     chan struct{}
	lifecycle sync.Mutex
	started   atomic.Bool
}

func serve(left pluginapi.LeftStrategy, right pluginapi.RightStrategy) error {
	h := &endpoint{left: left, right: right, ready: make(chan struct{})}
	if len(os.Args) == 2 && os.Args[1] == "--describe" {
		d, err := h.describe(context.Background())
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(d)
	}
	h.peer = ipc.NewPeer(os.Stdin, os.Stdout, h.handle)
	close(h.ready)
	<-h.peer.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.stop(ctx)
}
func (h *endpoint) describe(ctx context.Context) (pluginapi.Descriptor, error) {
	if h.left != nil {
		return h.left.Describe(ctx)
	}
	return h.right.Describe(ctx)
}
func (h *endpoint) stop(ctx context.Context) error {
	h.lifecycle.Lock()
	defer h.lifecycle.Unlock()
	h.started.Store(false)
	if h.left != nil {
		return h.left.Stop(ctx)
	}
	return h.right.Stop(ctx)
}
func (h *endpoint) handle(ctx context.Context, m string, raw json.RawMessage) (any, error) {
	switch m {
	case "describe":
		return h.describe(ctx)
	case "validate_config":
		if h.left != nil {
			return struct{}{}, h.left.ValidateConfig(ctx, raw)
		}
		return struct{}{}, h.right.ValidateConfig(ctx, raw)
	case "start":
		var spec pluginapi.InstanceSpec
		if err := pluginapi.Decode(raw, &spec); err != nil {
			return nil, err
		}
		h.lifecycle.Lock()
		defer h.lifecycle.Unlock()
		if h.started.Load() {
			return nil, pluginapi.Fail("STATE_CONFLICT", "instance already started")
		}
		var ready pluginapi.Ready
		var err error
		if h.left != nil {
			ready, err = h.left.Start(ctx, spec, func(ctx context.Context, q pluginapi.Request) (json.RawMessage, error) {
				select {
				case <-h.ready:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				var result json.RawMessage
				err := h.peer.Call(ctx, "dispatch", q, &result)
				return result, err
			})
		} else {
			ready, err = h.right.Start(ctx, spec)
		}
		if err == nil {
			h.started.Store(true)
		}
		return ready, err
	case "stop":
		return struct{}{}, h.stop(ctx)
	case "execute":
		if h.right == nil || !h.started.Load() {
			return nil, pluginapi.Fail("STATE_CONFLICT", "right instance not started")
		}
		var q pluginapi.Request
		if err := pluginapi.Decode(raw, &q); err != nil {
			return nil, err
		}
		return h.right.Execute(ctx, q)
	case "complete":
		if h.right == nil || !h.started.Load() {
			return nil, pluginapi.Fail("STATE_CONFLICT", "right instance not started")
		}
		var r pluginapi.Resolution
		if err := pluginapi.Decode(raw, &r); err != nil {
			return nil, err
		}
		return h.right.Complete(ctx, r)
	case "prepare":
		p, ok := h.right.(pluginapi.ScenarioPreparer)
		if !ok {
			return nil, pluginapi.Fail("UNSUPPORTED", "scenario.prepare not implemented")
		}
		if h.started.Load() {
			return nil, pluginapi.Fail("STATE_CONFLICT", "prepare requires a preparation-only process")
		}
		var s pluginapi.PreparationSpec
		if err := pluginapi.Decode(raw, &s); err != nil {
			return nil, err
		}
		return p.Prepare(ctx, s)
	case "export":
		p, ok := h.right.(pluginapi.ScenarioExporter)
		if !ok {
			return nil, pluginapi.Fail("UNSUPPORTED", "scenario.export not implemented")
		}
		var s pluginapi.ExportSpec
		if err := pluginapi.Decode(raw, &s); err != nil {
			return nil, err
		}
		return p.Export(ctx, s)
	case "verify":
		p, ok := h.right.(pluginapi.ScenarioVerifier)
		if !ok {
			return nil, pluginapi.Fail("UNSUPPORTED", "scenario.verify not implemented")
		}
		return p.Verify(ctx)
	}
	return nil, pluginapi.Fail("UNSUPPORTED", fmt.Sprintf("unknown plugin method %q", m))
}
