package binding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"xmock.local/x-mock-mcp/pluginapi"
)

type ManagedLeft interface {
	pluginapi.LeftStrategy
	Close() error
	Done() <-chan struct{}
}
type ManagedRight interface {
	pluginapi.RightStrategy
	Close() error
	Done() <-chan struct{}
}
type Factory interface {
	NewLeft(context.Context, pluginapi.Reference) (ManagedLeft, error)
	NewRight(context.Context, pluginapi.Reference) (ManagedRight, error)
}
type Config struct {
	Left             pluginapi.Reference       `json:"left"`
	Right            pluginapi.Reference       `json:"right"`
	LeftConfig       json.RawMessage           `json:"left_config"`
	RightConfig      json.RawMessage           `json:"right_config"`
	Contract         pluginapi.Contract        `json:"contract"`
	EnvironmentID    string                    `json:"environment_id"`
	ResourceID       string                    `json:"resource_id"`
	Scenario         json.RawMessage           `json:"scenario"`
	ScenarioVersion  int64                     `json:"scenario_version"`
	RequestTimeoutMS int64                     `json:"request_timeout_ms"`
	DataStrategy     pluginapi.DataStrategy    `json:"-"`
	DataStrategyName string                    `json:"data_strategy,omitempty"`
	OutcomeObserver  pluginapi.OutcomeObserver `json:"-"`
	OnFailure        func(error)               `json:"-"`
}
type Binding struct {
	ID            string              `json:"binding_id"`
	EnvironmentID string              `json:"environment_id"`
	ResourceID    string              `json:"resource_id"`
	Left          pluginapi.Reference `json:"left"`
	Right         pluginapi.Reference `json:"right"`
	Contract      pluginapi.Contract  `json:"contract"`
	Endpoints     map[string]string   `json:"endpoints"`
}
type entry struct {
	binding Binding
	config  Config
	left    ManagedLeft
	right   ManagedRight
	ctx     context.Context
	cancel  context.CancelFunc
	active  atomic.Bool
	mu      sync.Mutex
	runID   string
}
type Manager struct {
	mu       sync.Mutex
	factory  Factory
	entries  map[string]*entry
	failures map[string]error
}

func New(factory Factory) *Manager {
	return &Manager{factory: factory, entries: map[string]*entry{}, failures: map[string]error{}}
}
func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func negotiate(left, right pluginapi.Descriptor, want pluginapi.Contract) (pluginapi.Contract, error) {
	var lc, rc *pluginapi.Contract
	for _, c := range left.Contracts {
		if c.ID == want.ID && c.Version == want.Version {
			v := c
			lc = &v
		}
	}
	for _, c := range right.Contracts {
		if c.ID == want.ID && c.Version == want.Version {
			v := c
			rc = &v
		}
	}
	if lc == nil || rc == nil {
		return want, pluginapi.Fail("CONTRACT_MISMATCH", "left and right must support the requested contract version")
	}
	contains := func(xs []string, v string) bool {
		for _, x := range xs {
			if x == v {
				return true
			}
		}
		return false
	}
	caps := []string{}
	for _, c := range lc.Capabilities {
		if contains(rc.Capabilities, c) {
			caps = append(caps, c)
		}
	}
	for _, c := range want.Capabilities {
		if !contains(caps, c) {
			return want, pluginapi.Fail("CONTRACT_MISMATCH", "required capability absent from intersection")
		}
	}
	sort.Strings(caps)
	want.Capabilities = caps
	return want, nil
}
func (m *Manager) Create(ctx context.Context, c Config) (out Binding, err error) {
	if c.Left.Role != pluginapi.Left || c.Right.Role != pluginapi.Right {
		return out, pluginapi.Invalid("left/right plugin roles are fixed")
	}
	if err = c.Left.Validate(); err != nil {
		return out, err
	}
	if err = c.Right.Validate(); err != nil {
		return out, err
	}
	if c.EnvironmentID == "" || c.ResourceID == "" {
		return out, pluginapi.Invalid("environment and resource required")
	}
	if c.RequestTimeoutMS <= 0 {
		c.RequestTimeoutMS = 10000
	}
	e := &entry{config: c}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	success := false
	defer func() {
		if !success {
			m.cleanup(e)
		}
	}()
	e.right, err = m.factory.NewRight(ctx, c.Right)
	if err != nil {
		return out, err
	}
	e.left, err = m.factory.NewLeft(ctx, c.Left)
	if err != nil {
		return out, err
	}
	rd, err := e.right.Describe(ctx)
	if err != nil {
		return out, err
	}
	ld, err := e.left.Describe(ctx)
	if err != nil {
		return out, err
	}
	effective, err := negotiate(ld, rd, c.Contract)
	if err != nil {
		return out, err
	}
	if len(c.LeftConfig) == 0 {
		c.LeftConfig = json.RawMessage(`{}`)
	}
	if len(c.RightConfig) == 0 {
		c.RightConfig = json.RawMessage(`{}`)
	}
	if err = e.right.ValidateConfig(ctx, c.RightConfig); err != nil {
		return out, err
	}
	if err = e.left.ValidateConfig(ctx, c.LeftConfig); err != nil {
		return out, err
	}
	out = Binding{ID: ID(), EnvironmentID: c.EnvironmentID, ResourceID: c.ResourceID, Left: c.Left, Right: c.Right, Contract: effective}
	e.binding = out
	spec := pluginapi.InstanceSpec{InstanceID: ID(), EnvironmentID: c.EnvironmentID, BindingID: out.ID, ResourceID: c.ResourceID, Contract: effective, Config: c.RightConfig, Scenario: c.Scenario, ScenarioVersion: c.ScenarioVersion}
	spec.DataStrategy = c.DataStrategyName
	ready, err := e.right.Start(ctx, spec)
	if err != nil {
		return out, err
	}
	if ready.InstanceID != spec.InstanceID {
		return out, pluginapi.Invalid("right READY instance mismatch")
	}
	spec.InstanceID = ID()
	spec.Config = c.LeftConfig
	ready, err = e.left.Start(ctx, spec, func(ctx context.Context, q pluginapi.Request) (json.RawMessage, error) { return m.dispatch(e, ctx, q) })
	if err != nil {
		return out, err
	}
	if ready.InstanceID != spec.InstanceID || len(ready.Endpoints) == 0 {
		return out, pluginapi.Invalid("left READY identity/endpoints invalid")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	out.Endpoints = ready.Endpoints
	e.binding = out
	e.active.Store(true)
	m.mu.Lock()
	m.entries[out.ID] = e
	m.mu.Unlock()
	success = true
	go func() {
		select {
		case <-e.ctx.Done():
			return
		case <-e.left.Done():
		case <-e.right.Done():
		}
		if e.ctx.Err() != nil {
			return
		}
		failure := pluginapi.Fail("PLUGIN_EXITED", "plugin process or IPC exited")
		m.mu.Lock()
		m.failures[e.binding.ID] = failure
		m.mu.Unlock()
		if e.config.OnFailure != nil {
			e.config.OnFailure(failure)
		}
		m.Destroy(context.Background(), e.binding.ID)
	}()
	return out, nil
}
func (m *Manager) cleanup(e *entry) {
	e.active.Store(false)
	e.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e.left != nil {
		_ = e.left.Stop(ctx)
		_ = e.left.Close()
	}
	if e.right != nil {
		_ = e.right.Stop(ctx)
		_ = e.right.Close()
	}
}
func (m *Manager) Destroy(ctx context.Context, id string) error {
	m.mu.Lock()
	e := m.entries[id]
	delete(m.entries, id)
	m.mu.Unlock()
	if e != nil {
		m.cleanup(e)
	}
	return nil
}
func (m *Manager) List() []Binding {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Binding{}
	for _, e := range m.entries {
		out = append(out, e.binding)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (m *Manager) Failure(id string) error { m.mu.Lock(); defer m.mu.Unlock(); return m.failures[id] }
func (m *Manager) AssignRun(id, runID string) error {
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e == nil {
		return pluginapi.Fail("STATE_CONFLICT", "binding unavailable")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.runID != "" {
		return pluginapi.Fail("STATE_CONFLICT", "binding already belongs to a run")
	}
	e.runID = runID
	return nil
}
func (m *Manager) Right(id string) (ManagedRight, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil {
		return e.right, nil
	}
	return nil, pluginapi.Fail("STATE_CONFLICT", "binding unavailable")
}
func (m *Manager) Dispatch(ctx context.Context, q pluginapi.Request) (json.RawMessage, error) {
	m.mu.Lock()
	e := m.entries[q.BindingID]
	m.mu.Unlock()
	if e == nil {
		return nil, pluginapi.Fail("STATE_CONFLICT", "binding unavailable")
	}
	return m.dispatch(e, ctx, q)
}
func (m *Manager) dispatch(e *entry, ctx context.Context, q pluginapi.Request) (out json.RawMessage, err error) {
	if !e.active.Load() {
		return nil, pluginapi.Fail("STATE_CONFLICT", "binding not ready")
	}
	q.EnvironmentID = e.binding.EnvironmentID
	q.BindingID = e.binding.ID
	q.ResourceID = e.binding.ResourceID
	q.ContractID = e.binding.Contract.ID
	q.ContractVersion = e.binding.Contract.Version
	q.ScenarioVersion = e.config.ScenarioVersion
	e.mu.Lock()
	q.RunID = e.runID
	e.mu.Unlock()
	q.ID = ID()
	q.StartedUnixMS = time.Now().UnixMilli()
	deadline := time.Now().Add(time.Duration(e.config.RequestTimeoutMS) * time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	q.DeadlineUnixMS = deadline.UnixMilli()
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	defer func() {
		if e.config.OutcomeObserver != nil {
			e.config.OutcomeObserver(q, out, err)
		}
	}()
	decision, err := e.right.Execute(ctx, q)
	if err != nil {
		return nil, err
	}
	if err = decision.Validate(); err != nil {
		return nil, err
	}
	switch decision.Kind {
	case "completed":
		return decision.Payload, nil
	case "unsupported":
		return nil, decision.Error
	case "needs_data":
		need := *decision.Need
		if need.RequestID != q.ID {
			return nil, pluginapi.Fail("STATE_CONFLICT", "right returned a different request identity")
		}
		if need.DeadlineUnixMS > q.DeadlineUnixMS {
			need.DeadlineUnixMS = q.DeadlineUnixMS
		}
		ctx, cancelNeed := context.WithDeadline(ctx, time.UnixMilli(need.DeadlineUnixMS))
		defer cancelNeed()
		if e.config.DataStrategy == nil {
			return nil, pluginapi.Fail("UNMATCHED_REQUEST", "no data strategy configured")
		}
		candidate, err := e.config.DataStrategy.Resolve(ctx, need)
		if err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		return e.right.Complete(ctx, pluginapi.Resolution{RequestID: q.ID, Continuation: need.Continuation, StateVersion: need.StateVersion, Payload: candidate})
	}
	return nil, pluginapi.Invalid("unreachable decision")
}

// Seal keeps the right process available for verified export while preventing
// application traffic from mutating a completed run.
func (m *Manager) Seal(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil {
		e.active.Store(false)
		e.cancel()
	}
}
