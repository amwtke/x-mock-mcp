package main

import (
	"context"
	"encoding/json"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func TestSystemQueriesAreDeterministicAndUnknownSettingsFail(t *testing.T) {
	s, _ := stateFixture(t)
	ctx := context.Background()
	raw, known, err := s.system(ctx, "c", "SELECT @@session.autocommit AS autocommit, @@session.transaction_isolation AS transaction_isolation", "app")
	if !known || err != nil {
		t.Fatal(known, err)
	}
	var rows mysqlv1.Rows
	json.Unmarshal(raw, &rows)
	if len(rows.Rows) != 1 || len(rows.Columns) != 2 || string(rows.Rows[0][1]) != `"READ-COMMITTED"` {
		t.Fatal(rows)
	}
	if _, known, err = s.system(ctx, "c", "SET strange_variable = 42", "app"); !known || err == nil {
		t.Fatal("unknown SET was accepted")
	}
	if _, known, err = s.system(ctx, "c", "SELECT @@strange_variable", "app"); !known || err == nil {
		t.Fatal("unknown variable was accepted")
	}
}
