package mysqlv1

import "encoding/json"

type Value struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type Query struct {
	Database    string  `json:"database"`
	SQL         string  `json:"sql"`
	Params      []Value `json:"params"`
	StatementID string  `json:"statement_id,omitempty"`
}

type Metadata struct {
	StatementID string   `json:"statement_id"`
	Parameters  []Column `json:"parameters"`
	Columns     []Column `json:"columns"`
}

type Rows struct {
	Kind    string              `json:"kind"`
	Columns []Column            `json:"columns"`
	Rows    [][]json.RawMessage `json:"rows"`
	Status  SessionStatus       `json:"status"`
}

type SessionStatus struct {
	Autocommit    bool `json:"autocommit"`
	InTransaction bool `json:"in_transaction"`
}

type OK struct {
	Kind         string        `json:"kind"`
	AffectedRows string        `json:"affected_rows"`
	LastInsertID string        `json:"last_insert_id"`
	Status       SessionStatus `json:"status"`
	Warnings     uint16        `json:"warnings"`
}

type EntityFill struct {
	SlotID string             `json:"slot_id"`
	Rows   []map[string]Value `json:"rows"`
}

type Error struct {
	Kind     string `json:"kind"`
	Number   uint16 `json:"number"`
	SQLState string `json:"sql_state"`
	Message  string `json:"message"`
}
