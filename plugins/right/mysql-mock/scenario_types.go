package main

import (
	"encoding/json"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

type Source struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}
type QAStep struct {
	ID       string `json:"id"`
	Action   string `json:"action"`
	Expected string `json:"expected"`
}
type QA struct {
	ID                string            `json:"id"`
	Goal              string            `json:"goal"`
	NaturalLanguage   string            `json:"natural_language"`
	StartPage         string            `json:"start_page"`
	Role              string            `json:"role"`
	InitialState      string            `json:"initial_state"`
	DataPolicy        string            `json:"data_policy"`
	WriteRules        string            `json:"write_rules"`
	Coverage          string            `json:"coverage"`
	Steps             []QAStep          `json:"steps"`
	Fixed             map[string]string `json:"fixed"`
	Ambiguities       []string          `json:"ambiguities,omitempty"`
	InitialAssertions []StateAssertion  `json:"initial_assertions,omitempty"`
	FinalAssertions   []StateAssertion  `json:"final_assertions,omitempty"`
}
type Input struct {
	QA      QA       `json:"qa"`
	Sources []Source `json:"sources"`
}
type StepBinding struct {
	StepID     string   `json:"step_id"`
	API        string   `json:"api"`
	SourcePath string   `json:"source_path"`
	Statements []string `json:"statements"`
}
type APIExpectation struct {
	StepID     string                     `json:"step_id"`
	Method     string                     `json:"method"`
	Path       string                     `json:"path"`
	Status     int                        `json:"status"`
	Assertions map[string]json.RawMessage `json:"assertions,omitempty"`
}
type DataBinding struct {
	QAKey  string `json:"qa_key"`
	Table  string `json:"table"`
	Key    string `json:"key"`
	Column string `json:"column"`
}
type Entity map[string]mysqlv1.Value
type Field struct {
	mysqlv1.Column
	MaxLength     int            `json:"max_length,omitempty"`
	Default       *mysqlv1.Value `json:"default,omitempty"`
	AutoIncrement bool           `json:"auto_increment,omitempty"`
}
type ForeignKey struct {
	Columns    []string `json:"columns"`
	Table      string   `json:"table"`
	References []string `json:"references"`
}
type Table struct {
	Name        string       `json:"name"`
	Columns     []Field      `json:"columns"`
	PrimaryKey  []string     `json:"primary_key"`
	Unique      [][]string   `json:"unique,omitempty"`
	ForeignKeys []ForeignKey `json:"foreign_keys,omitempty"`
}
type Parameter struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Nullable bool            `json:"nullable"`
	Allowed  []mysqlv1.Value `json:"allowed"`
}
type Statement struct {
	ID         string        `json:"id"`
	SQL        string        `json:"sql"`
	SourcePath string        `json:"source_path"`
	Parameters []Parameter   `json:"parameters"`
	Plan       *Plan         `json:"plan,omitempty"`
	Cases      []FixtureCase `json:"cases,omitempty"`
}
type FixtureCase struct {
	Params []mysqlv1.Value     `json:"params"`
	Rows   [][]json.RawMessage `json:"rows"`
}
type Slot struct {
	ID           string                   `json:"id"`
	Table        string                   `json:"table"`
	Keys         []string                 `json:"keys"`
	Fixed        map[string]mysqlv1.Value `json:"fixed"`
	Seed         int64                    `json:"seed"`
	Materialized bool                     `json:"materialized"`
}
type DatabaseScenario struct {
	Database        string              `json:"database"`
	Mode            string              `json:"mode"`
	Tables          []Table             `json:"tables,omitempty"`
	Initial         map[string][]Entity `json:"initial"`
	Statements      []Statement         `json:"statements"`
	GenerationSlots []Slot              `json:"generation_slots,omitempty"`
	NextIDs         map[string]string   `json:"next_ids,omitempty"`
	Transaction     string              `json:"transaction,omitempty"`
}
type CountRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}
type StateAssertion struct {
	Table  string `json:"table"`
	Where  Entity `json:"where"`
	Count  int    `json:"count"`
	Fields Entity `json:"fields,omitempty"`
}
type VerificationSpec struct {
	ExpectCalls map[string]CountRange `json:"expect_calls,omitempty"`
	FinalState  []StateAssertion      `json:"final_state,omitempty"`
}
type Replay struct {
	Seed               int64  `json:"seed"`
	Clock              string `json:"clock"`
	RequiresGeneration bool   `json:"requires_generation"`
}
type Bundle struct {
	QAContract       QA               `json:"qa_contract"`
	Evidence         []Source         `json:"evidence"`
	StepBindings     []StepBinding    `json:"step_bindings"`
	APIExpectations  []APIExpectation `json:"api_expectations"`
	DataBindings     []DataBinding    `json:"data_bindings"`
	DatabaseScenario DatabaseScenario `json:"database_scenario"`
	Verification     VerificationSpec `json:"verification"`
	Replay           Replay           `json:"replay"`
	Preview          []PreviewStep    `json:"preview,omitempty"`
}

type PreviewStep struct {
	StatementID string          `json:"statement_id,omitempty"`
	Params      []mysqlv1.Value `json:"params,omitempty"`
	Control     string          `json:"control,omitempty"`
	Connection  string          `json:"connection"`
}
type Operand struct {
	Parameter *int           `json:"parameter,omitempty"`
	Literal   *mysqlv1.Value `json:"literal,omitempty"`
}
type Predicate struct {
	Column string  `json:"column"`
	Value  Operand `json:"value"`
}
type Assignment struct {
	Column  string  `json:"column"`
	AddFrom string  `json:"add_from,omitempty"`
	Value   Operand `json:"value"`
}
type Order struct {
	Column     string `json:"column"`
	Descending bool   `json:"descending"`
}
type Plan struct {
	Kind        string           `json:"kind"`
	Table       string           `json:"table"`
	Projection  []string         `json:"projection,omitempty"`
	Columns     []mysqlv1.Column `json:"columns"`
	Predicates  []Predicate      `json:"predicates,omitempty"`
	Assignments []Assignment     `json:"assignments,omitempty"`
	Order       []Order          `json:"order,omitempty"`
	Limit       *uint64          `json:"limit,omitempty"`
	Parameters  []mysqlv1.Column `json:"parameters"`
}

func (t Table) Field(name string) (Field, bool) {
	for _, f := range t.Columns {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}
