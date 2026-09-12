package pluginapi

import (
	"context"
	"encoding/json"
)

type PreparationContract struct {
	InputSchema     json.RawMessage `json:"input_schema"`
	CandidateSchema json.RawMessage `json:"candidate_schema"`
	Guide           string          `json:"guide"`
}

type PreparationSpec struct {
	ProjectRoot string          `json:"project_root"`
	Input       json.RawMessage `json:"input"`
	Candidate   json.RawMessage `json:"candidate,omitempty"`
}

type PreparationIssue struct {
	Code    string   `json:"code"`
	Path    string   `json:"path"`
	Message string   `json:"message"`
	StepIDs []string `json:"step_ids,omitempty"`
}

type PreparationReport struct {
	Ready          bool               `json:"ready"`
	MissingInputs  []PreparationIssue `json:"missing_inputs"`
	Ambiguities    []PreparationIssue `json:"ambiguities"`
	Unsupported    []PreparationIssue `json:"unsupported"`
	Diagnostics    []PreparationIssue `json:"diagnostics"`
	Instructions   string             `json:"instructions,omitempty"`
	CompiledBody   json.RawMessage    `json:"compiled_body,omitempty"`
	InputDigest    string             `json:"input_digest,omitempty"`
	CompiledDigest string             `json:"compiled_digest,omitempty"`
}

type ScenarioPreparer interface {
	Prepare(context.Context, PreparationSpec) (PreparationReport, error)
}
