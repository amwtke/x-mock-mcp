package pluginapi

import (
	"context"
	"encoding/json"
)

type InstanceSpec struct {
	InstanceID      string          `json:"instance_id"`
	EnvironmentID   string          `json:"environment_id"`
	BindingID       string          `json:"binding_id"`
	ResourceID      string          `json:"resource_id"`
	Contract        Contract        `json:"contract"`
	Config          json.RawMessage `json:"config"`
	Scenario        json.RawMessage `json:"scenario"`
	ScenarioVersion int64           `json:"scenario_version"`
}

type Request struct {
	ID              string          `json:"request_id"`
	EnvironmentID   string          `json:"environment_id"`
	RunID           string          `json:"run_id"`
	BindingID       string          `json:"binding_id"`
	ResourceID      string          `json:"resource_id"`
	ConnectionID    string          `json:"connection_id"`
	ContractID      string          `json:"contract_id"`
	ContractVersion int             `json:"contract_version"`
	ScenarioVersion int64           `json:"scenario_version"`
	DeadlineUnixMS  int64           `json:"deadline_unix_ms"`
	Operation       string          `json:"operation"`
	Payload         json.RawMessage `json:"payload"`
}

type Need struct {
	RequestID      string          `json:"request_id"`
	Continuation   string          `json:"continuation_token"`
	StateVersion   int64           `json:"state_version"`
	DeadlineUnixMS int64           `json:"deadline_unix_ms"`
	Schema         json.RawMessage `json:"schema"`
	Context        json.RawMessage `json:"context"`
}

type Decision struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Need    *Need           `json:"need,omitempty"`
	Error   *Failure        `json:"error,omitempty"`
}

type Resolution struct {
	RequestID    string          `json:"request_id"`
	Continuation string          `json:"continuation_token"`
	StateVersion int64           `json:"state_version"`
	Payload      json.RawMessage `json:"payload"`
}

type Ready struct {
	InstanceID string            `json:"instance_id"`
	Endpoints  map[string]string `json:"endpoints"`
}

type Dispatch func(context.Context, Request) (json.RawMessage, error)

type OutcomeObserver func(Request, json.RawMessage, error)

type LeftStrategy interface {
	Describe(context.Context) (Descriptor, error)
	ValidateConfig(context.Context, json.RawMessage) error
	Start(context.Context, InstanceSpec, Dispatch) (Ready, error)
	Stop(context.Context) error
}

type RightStrategy interface {
	Describe(context.Context) (Descriptor, error)
	ValidateConfig(context.Context, json.RawMessage) error
	Start(context.Context, InstanceSpec) (Ready, error)
	Execute(context.Context, Request) (Decision, error)
	Complete(context.Context, Resolution) (json.RawMessage, error)
	Stop(context.Context) error
}

type DataStrategy interface {
	Resolve(context.Context, Need) (json.RawMessage, error)
}

type Capture struct {
	Request           Request         `json:"request"`
	Candidate         json.RawMessage `json:"candidate,omitempty"`
	GenerationContext json.RawMessage `json:"generation_context,omitempty"`
	Response          json.RawMessage `json:"response"`
}

type ExportSpec struct {
	Snapshot json.RawMessage `json:"snapshot"`
	Captures []Capture       `json:"captures"`
}

type Verification struct {
	Passed bool      `json:"passed"`
	Issues []Failure `json:"issues"`
}

type ScenarioExporter interface {
	Export(context.Context, ExportSpec) (json.RawMessage, error)
}

type ScenarioVerifier interface {
	Verify(context.Context) (Verification, error)
}
