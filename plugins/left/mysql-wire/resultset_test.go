package main

import (
	"encoding/json"
	"github.com/go-mysql-org/go-mysql/mysql"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func TestResultsetEncodingPreservesTypesAndNulls(t *testing.T) {
	columns := []mysqlv1.Column{{Name: "id", Type: "BIGINT"}, {Name: "name", Type: "VARCHAR", Nullable: true}}
	rows := mysqlv1.Rows{Kind: "rows", Columns: columns, Rows: [][]json.RawMessage{{json.RawMessage(`"9007199254740993"`), json.RawMessage(`null`)}, {json.RawMessage(`"7"`), json.RawMessage(`"键盘"`)}}}
	for _, binary := range []bool{false, true} {
		result, err := encodeResultset(rows, binary)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := result.RowDatas[0].Parse(result.Fields, binary, nil)
		if err != nil {
			t.Fatal(err)
		}
		if parsed[0].AsInt64() != 9007199254740993 || parsed[1].Type != mysql.FieldValueTypeNull {
			t.Fatal(parsed)
		}
		parsed, err = result.RowDatas[1].Parse(result.Fields, binary, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(parsed[1].AsString()) != "键盘" {
			t.Fatal(parsed)
		}
		rowsCopy := rows
		rowsCopy.Rows = nil
		empty, err := encodeResultset(rowsCopy, binary)
		if err != nil {
			t.Fatal(err)
		}
		if empty.Fields[0].Type != mysql.MYSQL_TYPE_LONGLONG || empty.Fields[1].Flag&mysql.NOT_NULL_FLAG != 0 {
			t.Fatal("empty result lost metadata")
		}
	}
}
