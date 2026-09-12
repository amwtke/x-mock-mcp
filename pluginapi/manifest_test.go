package pluginapi

import (
	"encoding/json"
	"testing"
)

func descriptor() Descriptor {
	return Descriptor{Ref: Reference{ID: "echo", Version: "0.1.0", Role: Right}, APIMajor: 1,
		Contracts: []Contract{{ID: "echo", Version: 1}}, ConfigSchema: json.RawMessage(`{"type":"object"}`)}
}

func TestManifestValidation(t *testing.T) {
	for _, role := range []Role{Left, Right} {
		d := descriptor()
		d.Ref.Role = role
		if err := d.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	tests := map[string]func(*Descriptor){
		"traversal":           func(d *Descriptor) { d.Ref.ID = "../mysql" },
		"empty":               func(d *Descriptor) { d.Ref.ID = "" },
		"role":                func(d *Descriptor) { d.Ref.Role = "both" },
		"floating-version":    func(d *Descriptor) { d.Ref.Version = "latest" },
		"api":                 func(d *Descriptor) { d.APIMajor = 2 },
		"duplicate-contract":  func(d *Descriptor) { d.Contracts = append(d.Contracts, d.Contracts[0]) },
		"invalid-schema":      func(d *Descriptor) { d.ConfigSchema = json.RawMessage(`null`) },
		"missing-preparation": func(d *Descriptor) { d.Features = []string{"scenario.prepare"} },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			d := descriptor()
			change(&d)
			if d.Validate() == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}

func TestDecisionRejectsConflictingOutcomes(t *testing.T) {
	for _, d := range []Decision{
		{Kind: "completed"},
		{Kind: "completed", Payload: json.RawMessage(`{"value":1}`), Need: &Need{RequestID: "r"}},
		{Kind: "unknown"},
		{Kind: "unsupported"},
	} {
		if d.Validate() == nil {
			t.Fatalf("accepted invalid decision: %+v", d)
		}
	}
	if err := (Decision{Kind: "completed", Payload: json.RawMessage(`{"value":1}`)}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparationCannotClaimReadyWithMissingEvidence(t *testing.T) {
	report := PreparationReport{Ready: true, MissingInputs: []PreparationIssue{{Code: "MISSING", Path: "ddl"}}}
	if report.Validate() == nil {
		t.Fatal("incomplete candidate declared ready")
	}
	report = PreparationReport{Ready: false, CompiledBody: json.RawMessage(`{}`)}
	if report.Validate() == nil {
		t.Fatal("non-ready report exposes executable body")
	}
	if err := (PreparationReport{Ready: false, MissingInputs: []PreparationIssue{{Code: "MISSING", Path: "qa"}}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
