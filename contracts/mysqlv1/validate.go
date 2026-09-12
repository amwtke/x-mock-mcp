package mysqlv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"unicode/utf8"
)

func Int(n int64) Value {
	return Value{Type: "BIGINT", Value: json.RawMessage(strconv.Quote(strconv.FormatInt(n, 10)))}
}
func Text(s string) Value    { b, _ := json.Marshal(s); return Value{Type: "VARCHAR", Value: b} }
func Null(typ string) Value  { return Value{Type: typ, Value: json.RawMessage(`null`)} }
func (v Value) IsNull() bool { return bytes.Equal(bytes.TrimSpace(v.Value), []byte("null")) }
func (v Value) String() (string, error) {
	var s string
	err := json.Unmarshal(v.Value, &s)
	return s, err
}
func (v Value) Int64() (int64, error) {
	s, err := v.String()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}
func ValidateValue(column Column, value Value) error {
	if value.Type != column.Type {
		return fmt.Errorf("column %s: expected %s, got %s", column.Name, column.Type, value.Type)
	}
	if column.Type != "BIGINT" && column.Type != "VARCHAR" {
		return fmt.Errorf("unsupported type %s", column.Type)
	}
	if value.IsNull() {
		if !column.Nullable {
			return fmt.Errorf("column %s does not allow NULL", column.Name)
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(value.Value, &s); err != nil {
		return fmt.Errorf("column %s must use a typed string: %w", column.Name, err)
	}
	if column.Type == "BIGINT" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != s {
			return fmt.Errorf("column %s: invalid canonical int64", column.Name)
		}
	} else if !utf8.ValidString(s) {
		return fmt.Errorf("invalid UTF-8")
	}
	return nil
}
func ValidateRows(metadata Metadata, rows Rows) error {
	if rows.Kind != "rows" || !reflect.DeepEqual(metadata.Columns, rows.Columns) {
		return fmt.Errorf("result column metadata differs from prepared declaration")
	}
	if len(rows.Rows) > 1000 {
		return fmt.Errorf("row count exceeds 1000")
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("result exceeds 1 MiB")
	}
	for _, row := range rows.Rows {
		if len(row) != len(metadata.Columns) {
			return fmt.Errorf("row arity does not match columns")
		}
		for i, value := range row {
			if err = ValidateValue(metadata.Columns[i], Value{Type: metadata.Columns[i].Type, Value: value}); err != nil {
				return err
			}
		}
	}
	return nil
}
func (o OK) Validate() error {
	if o.Kind != "ok" {
		return fmt.Errorf("not an OK result")
	}
	for _, s := range []string{o.AffectedRows, o.LastInsertID} {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != s {
			return fmt.Errorf("invalid uint64 counter")
		}
	}
	return nil
}
func Encode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
