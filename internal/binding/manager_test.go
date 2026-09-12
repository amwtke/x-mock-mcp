package binding

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"xmock.local/x-mock-mcp/pluginapi"
)

type fakeBase struct {
	desc             pluginapi.Descriptor
	done             chan struct{}
	once             sync.Once
	started, stopped bool
}

func (b *fakeBase) Describe(context.Context) (pluginapi.Descriptor, error) { return b.desc, nil }
func (b *fakeBase) ValidateConfig(context.Context, json.RawMessage) error  { return nil }
func (b *fakeBase) Stop(context.Context) error                             { b.stopped = true; return nil }
func (b *fakeBase) Close() error                                           { b.once.Do(func() { close(b.done) }); return nil }
func (b *fakeBase) Done() <-chan struct{}                                  { return b.done }

type fakeLeft struct {
	fakeBase
	fail     bool
	dispatch pluginapi.Dispatch
}

func (l *fakeLeft) Start(ctx context.Context, s pluginapi.InstanceSpec, d pluginapi.Dispatch) (pluginapi.Ready, error) {
	l.started = true
	l.dispatch = d
	if l.fail {
		return pluginapi.Ready{}, errors.New("port busy")
	}
	return pluginapi.Ready{InstanceID: s.InstanceID, Endpoints: map[string]string{"tcp": "127.0.0.1:13306"}}, nil
}

type fakeRight struct {
	fakeBase
	completions int
}

func (r *fakeRight) Start(ctx context.Context, s pluginapi.InstanceSpec) (pluginapi.Ready, error) {
	r.started = true
	return pluginapi.Ready{InstanceID: s.InstanceID}, nil
}
func (r *fakeRight) Execute(ctx context.Context, q pluginapi.Request) (pluginapi.Decision, error) {
	return pluginapi.Decision{Kind: "needs_data", Need: &pluginapi.Need{RequestID: q.ID, Continuation: "token", Schema: json.RawMessage(`{"type":"object"}`), DeadlineUnixMS: q.DeadlineUnixMS}}, nil
}
func (r *fakeRight) Complete(context.Context, pluginapi.Resolution) (json.RawMessage, error) {
	r.completions++
	return json.RawMessage(`{"value":"finished"}`), nil
}

type fakeFactory struct {
	left  *fakeLeft
	right *fakeRight
}

func (f fakeFactory) NewLeft(context.Context, pluginapi.Reference) (ManagedLeft, error) {
	return f.left, nil
}
func (f fakeFactory) NewRight(context.Context, pluginapi.Reference) (ManagedRight, error) {
	return f.right, nil
}

type data struct{}

func (data) Resolve(context.Context, pluginapi.Need) (json.RawMessage, error) {
	return json.RawMessage(`{"value":"candidate"}`), nil
}
func setup() (*Manager, *fakeLeft, *fakeRight, Config) {
	contract := pluginapi.Contract{ID: "echo", Version: 1, Capabilities: []string{"echo"}}
	left := &fakeLeft{fakeBase: fakeBase{desc: pluginapi.Descriptor{Contracts: []pluginapi.Contract{contract}}, done: make(chan struct{})}}
	right := &fakeRight{fakeBase: fakeBase{desc: pluginapi.Descriptor{Contracts: []pluginapi.Contract{contract}}, done: make(chan struct{})}}
	conf := Config{Left: pluginapi.Reference{ID: "echo", Version: "0.1.0", Role: pluginapi.Left}, Right: pluginapi.Reference{ID: "echo", Version: "0.1.0", Role: pluginapi.Right}, Contract: contract, EnvironmentID: "env", ResourceID: "resource", Scenario: json.RawMessage(`{}`), DataStrategy: data{}}
	return New(fakeFactory{left, right}), left, right, conf
}
func TestCreateRollsBack(t *testing.T) {
	m, l, r, c := setup()
	l.fail = true
	if _, err := m.Create(context.Background(), c); err == nil {
		t.Fatal("failed left published")
	}
	if !r.started || !r.stopped {
		t.Fatal("right was not rolled back")
	}
	select {
	case <-r.done:
	default:
		t.Fatal("right process not closed")
	}
	if len(m.List()) != 0 {
		t.Fatal("failed binding visible")
	}
}
func TestRouterOwnsFinalCompletion(t *testing.T) {
	m, _, r, c := setup()
	observed := 0
	c.OutcomeObserver = func(q pluginapi.Request, result json.RawMessage, err error) {
		observed++
		if err != nil {
			t.Error(err)
		}
	}
	b, err := m.Create(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Destroy(context.Background(), b.ID)
	result, err := m.Dispatch(context.Background(), pluginapi.Request{BindingID: b.ID, ID: "r1", Operation: "query", Payload: json.RawMessage(`{}`)})
	if err != nil || string(result) != `{"value":"finished"}` {
		t.Fatal(string(result), err)
	}
	if r.completions != 1 || observed != 1 {
		t.Fatal("completion or observer called incorrectly", r.completions, observed)
	}
}
func TestContractMismatchBeforeStart(t *testing.T) {
	m, l, r, c := setup()
	r.desc.Contracts[0].ID = "different"
	if _, err := m.Create(context.Background(), c); pluginapi.Code(err) != "CONTRACT_MISMATCH" {
		t.Fatal(err)
	}
	if l.started || r.started {
		t.Fatal("incompatible binding started")
	}
}
