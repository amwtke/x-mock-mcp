package mysqlv1

import (
	"encoding/json"
	"testing"
)

func TestBigintWireValueValidation(t *testing.T) {
	meta := Metadata{Columns: []Column{{Name: "id", Type: "BIGINT"}}}
	for _, tc := range []struct {
		name, value string
		bad         bool
	}{{"large", `"9007199254740993"`, false}, {"overflow", `"9223372036854775808"`, true}, {"unsafe-number", `9007199254740993`, true}, {"null", `null`, true}} {
		t.Run(tc.name, func(t *testing.T) {
			r := Rows{Kind: "rows", Columns: meta.Columns, Rows: [][]json.RawMessage{{json.RawMessage(tc.value)}}}
			if err := ValidateRows(meta, r); (err != nil) != tc.bad {
				t.Fatal(err)
			}
		})
	}
}
func TestRowsMetadataAndNulls(t *testing.T) {
	meta := Metadata{Columns: []Column{{Name: "name", Type: "VARCHAR", Nullable: true}}}
	for _, values := range [][][]json.RawMessage{nil, {{json.RawMessage(`null`)}}, {{json.RawMessage(`"键盘"`)}}} {
		if err := ValidateRows(meta, Rows{Kind: "rows", Columns: meta.Columns, Rows: values}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateRows(meta, Rows{Kind: "rows", Columns: []Column{{Name: "other", Type: "VARCHAR"}}}); err == nil {
		t.Fatal("metadata mismatch accepted")
	}
	if err := ValidateRows(meta, Rows{Kind: "rows", Columns: meta.Columns, Rows: [][]json.RawMessage{{}}}); err == nil {
		t.Fatal("row arity mismatch accepted")
	}
}
func TestOKCountersAndEntityFill(t *testing.T) {
	if err := (OK{Kind: "ok", AffectedRows: "1", LastInsertID: "5001"}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"-1", "18446744073709551616", "1.5"} {
		if (OK{Kind: "ok", AffectedRows: n, LastInsertID: "0"}).Validate() == nil {
			t.Fatal("invalid OK counter", n)
		}
	}
	if err := ValidateValue(Column{Name: "price", Type: "DECIMAL"}, Value{Type: "DECIMAL", Value: json.RawMessage(`"99.00"`)}); err == nil {
		t.Fatal("unsupported decimal silently accepted")
	}
}
