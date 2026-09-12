package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"xmock.local/x-mock-mcp/pluginapi"
)

func (p *Process) Describe(context.Context) (pluginapi.Descriptor, error) { return p.desc, nil }
func (p *Process) ValidateConfig(ctx context.Context, config json.RawMessage) error {
	return p.peer.Call(ctx, "validate_config", config, nil)
}
func (p *Process) Stop(ctx context.Context) error { return p.peer.Call(ctx, "stop", struct{}{}, nil) }

type LeftProxy struct {
	*Process
	mu       sync.Mutex
	dispatch pluginapi.Dispatch
}

func (l *LeftProxy) Start(ctx context.Context, s pluginapi.InstanceSpec, d pluginapi.Dispatch) (pluginapi.Ready, error) {
	l.mu.Lock()
	l.dispatch = d
	l.mu.Unlock()
	var r pluginapi.Ready
	err := l.peer.Call(ctx, "start", s, &r)
	return r, err
}

type RightProxy struct{ *Process }

func (r *RightProxy) Start(ctx context.Context, s pluginapi.InstanceSpec) (pluginapi.Ready, error) {
	var ready pluginapi.Ready
	err := r.peer.Call(ctx, "start", s, &ready)
	return ready, err
}
func (r *RightProxy) Execute(ctx context.Context, q pluginapi.Request) (pluginapi.Decision, error) {
	var d pluginapi.Decision
	err := r.peer.Call(ctx, "execute", q, &d)
	return d, err
}
func (r *RightProxy) Complete(ctx context.Context, s pluginapi.Resolution) (json.RawMessage, error) {
	var result json.RawMessage
	err := r.peer.Call(ctx, "complete", s, &result)
	return result, err
}
func (r *RightProxy) Prepare(ctx context.Context, s pluginapi.PreparationSpec) (pluginapi.PreparationReport, error) {
	var out pluginapi.PreparationReport
	if !r.desc.HasFeature("scenario.prepare") {
		return out, pluginapi.Fail("UNSUPPORTED", "plugin does not declare scenario.prepare")
	}
	err := r.peer.Call(ctx, "prepare", s, &out)
	if err == nil {
		err = out.Validate()
	}
	return out, err
}
func (r *RightProxy) Export(ctx context.Context, s pluginapi.ExportSpec) (json.RawMessage, error) {
	var out json.RawMessage
	if !r.desc.HasFeature("scenario.export") {
		return nil, pluginapi.Fail("UNSUPPORTED", "plugin does not declare scenario.export")
	}
	err := r.peer.Call(ctx, "export", s, &out)
	return out, err
}
func (r *RightProxy) Verify(ctx context.Context) (pluginapi.Verification, error) {
	var out pluginapi.Verification
	if !r.desc.HasFeature("scenario.verify") {
		return out, pluginapi.Fail("UNSUPPORTED", "plugin does not declare scenario.verify")
	}
	err := r.peer.Call(ctx, "verify", struct{}{}, &out)
	return out, err
}
