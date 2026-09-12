package mysqlv1

import (
	"encoding/json"
	"testing"
)

func TestSchemaRejectsUnexpectedQueryFields(t *testing.T) {
	schema := SchemaFor[Query]()
	if err := CheckSchema(schema, json.RawMessage(`{"database":"app","sql":"SELECT 1","params":[],"unexpected":true}`)); err == nil {
		t.Fatal("schema accepted unknown query field")
	}
	if err := CheckSchema(schema, json.RawMessage(`{"database":"app","sql":"SELECT 1","params":[]}`)); err != nil {
		t.Fatal(err)
	}
}
