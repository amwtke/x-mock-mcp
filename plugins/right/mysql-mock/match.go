package main

import (
	"fmt"
	"github.com/pingcap/tidb/pkg/parser"
	"reflect"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func matchStatement(db DatabaseScenario, sql string, args []mysqlv1.Value, prepare bool) (Statement, []mysqlv1.Value, error) {
	nodes, _, err := parser.New().ParseSQL(sql)
	if err != nil || len(nodes) != 1 {
		return Statement{}, nil, fmt.Errorf("unrecognized SQL")
	}
	visitor := &markers{}
	nodes[0].Accept(visitor)
	for _, statement := range db.Statements {
		candidate := Statement{SQL: sql}
		if len(visitor.nodes) > 0 {
			candidate.Parameters = statement.Parameters
		}
		actual, err := Compile(candidate, db.Tables, db.Database)
		if err != nil {
			continue
		}
		expected := statement.Plan
		if expected == nil {
			continue
		}
		values := make([]mysqlv1.Value, len(statement.Parameters))
		assigned := make([]bool, len(values))
		matchOperand := func(want, got Operand) bool {
			if want.Parameter == nil {
				return got.Parameter == nil && want.Literal != nil && got.Literal != nil && equalValue(*want.Literal, *got.Literal)
			}
			index := *want.Parameter
			var value mysqlv1.Value
			if got.Parameter != nil {
				if *got.Parameter != index {
					return false
				}
				if prepare {
					return true
				}
				if index >= len(args) {
					return false
				}
				value = args[index]
			} else if got.Literal != nil {
				value = *got.Literal
			} else {
				return false
			}
			if assigned[index] && !equalValue(values[index], value) {
				return false
			}
			values[index] = value
			assigned[index] = true
			return true
		}
		if expected.Kind != actual.Kind || expected.Table != actual.Table || !reflect.DeepEqual(expected.Projection, actual.Projection) || !reflect.DeepEqual(expected.Columns, actual.Columns) || !reflect.DeepEqual(expected.Order, actual.Order) || !reflect.DeepEqual(expected.Limit, actual.Limit) || len(expected.Predicates) != len(actual.Predicates) || len(expected.Assignments) != len(actual.Assignments) {
			continue
		}
		valid := true
		for i, want := range expected.Predicates {
			got := actual.Predicates[i]
			valid = valid && want.Column == got.Column && matchOperand(want.Value, got.Value)
		}
		for i, want := range expected.Assignments {
			got := actual.Assignments[i]
			valid = valid && want.Column == got.Column && want.AddFrom == got.AddFrom && matchOperand(want.Value, got.Value)
		}
		if !valid {
			continue
		}
		if !prepare {
			if len(visitor.nodes) > 0 && len(args) != len(values) {
				continue
			}
			for i, value := range values {
				if !assigned[i] || mysqlv1.ValidateValue(expected.Parameters[i], value) != nil {
					valid = false
					break
				}
				allowed := false
				for _, v := range statement.Parameters[i].Allowed {
					allowed = allowed || equalValue(v, value)
				}
				if !allowed {
					valid = false
					break
				}
			}
		}
		if valid {
			return statement, values, nil
		}
	}
	return Statement{}, nil, fmt.Errorf("SQL structure or typed parameters do not match a declared rule")
}
