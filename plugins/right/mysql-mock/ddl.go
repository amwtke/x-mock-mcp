package main

import (
	"fmt"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	pmysql "github.com/pingcap/tidb/pkg/parser/mysql"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
	"math"
	"strings"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func ParseDDL(sql string) ([]Table, error) {
	stmts, warnings, err := parser.New().ParseSQL(sql)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		return nil, fmt.Errorf("DDL parser warnings: %v", warnings)
	}
	tables := []Table{}
	seen := map[string]bool{}
	for _, stmt := range stmts {
		n, ok := stmt.(*ast.CreateTableStmt)
		if !ok || n.Select != nil || n.ReferTable != nil || n.Partition != nil {
			return nil, fmt.Errorf("only final CREATE TABLE definitions are supported")
		}
		t := Table{Name: n.Table.Name.O}
		if seen[t.Name] {
			return nil, fmt.Errorf("duplicate table %s", t.Name)
		}
		seen[t.Name] = true
		if len(n.Options) > 0 {
			return nil, fmt.Errorf("table options require an explicit supported implementation")
		}
		for _, col := range n.Cols {
			f := Field{Column: mysqlv1.Column{Name: col.Name.Name.O, Nullable: true}}
			if _, found := t.Field(f.Name); found {
				return nil, fmt.Errorf("duplicate column")
			}
			if pmysql.HasUnsignedFlag(col.Tp.GetFlag()) {
				return nil, fmt.Errorf("unsigned columns are unsupported")
			}
			switch col.Tp.GetType() {
			case pmysql.TypeLonglong:
				f.Type = "BIGINT"
			case pmysql.TypeVarchar:
				f.Type = "VARCHAR"
				f.MaxLength = col.Tp.GetFlen()
			default:
				return nil, fmt.Errorf("unsupported DDL type for %s", f.Name)
			}
			for _, opt := range col.Options {
				switch opt.Tp {
				case ast.ColumnOptionPrimaryKey:
					t.PrimaryKey = append(t.PrimaryKey, f.Name)
					f.Nullable = false
				case ast.ColumnOptionNotNull:
					f.Nullable = false
				case ast.ColumnOptionNull:
					f.Nullable = true
				case ast.ColumnOptionAutoIncrement:
					f.AutoIncrement = true
				case ast.ColumnOptionUniqKey:
					t.Unique = append(t.Unique, []string{f.Name})
				case ast.ColumnOptionDefaultValue:
					v, err := literalValue(opt.Expr, f.Type)
					if err != nil {
						return nil, err
					}
					f.Default = &v
				case ast.ColumnOptionComment:
				default:
					return nil, fmt.Errorf("unsupported column option for %s", f.Name)
				}
			}
			t.Columns = append(t.Columns, f)
		}
		for _, constraint := range n.Constraints {
			names := []string{}
			for _, key := range constraint.Keys {
				if key.Column == nil || key.Expr != nil || key.Length > 0 {
					return nil, fmt.Errorf("expression/prefix indexes unsupported")
				}
				names = append(names, key.Column.Name.O)
			}
			switch constraint.Tp {
			case ast.ConstraintPrimaryKey:
				if len(t.PrimaryKey) > 0 {
					return nil, fmt.Errorf("multiple primary keys")
				}
				t.PrimaryKey = names
			case ast.ConstraintUniq, ast.ConstraintUniqKey, ast.ConstraintUniqIndex:
				t.Unique = append(t.Unique, names)
			case ast.ConstraintForeignKey:
				if constraint.Refer == nil {
					return nil, fmt.Errorf("foreign reference missing")
				}
				fk := ForeignKey{Columns: names, Table: constraint.Refer.Table.Name.O}
				for _, part := range constraint.Refer.IndexPartSpecifications {
					if part.Column == nil {
						return nil, fmt.Errorf("unsupported foreign index")
					}
					fk.References = append(fk.References, part.Column.Name.O)
				}
				if constraint.Refer.OnDelete != nil && constraint.Refer.OnDelete.ReferOpt != ast.ReferOptionNoOption {
					return nil, fmt.Errorf("foreign cascades unsupported")
				}
				if constraint.Refer.OnUpdate != nil && constraint.Refer.OnUpdate.ReferOpt != ast.ReferOptionNoOption {
					return nil, fmt.Errorf("foreign cascades unsupported")
				}
				t.ForeignKeys = append(t.ForeignKeys, fk)
			case ast.ConstraintKey, ast.ConstraintIndex:
			default:
				return nil, fmt.Errorf("unsupported table constraint")
			}
		}
		if len(t.PrimaryKey) != 1 {
			return nil, fmt.Errorf("P0 requires one-column primary keys")
		}
		for i, f := range t.Columns {
			if f.Name == t.PrimaryKey[0] {
				f.Nullable = false
				t.Columns[i] = f
			}
			if f.AutoIncrement && (f.Name != t.PrimaryKey[0] || f.Type != "BIGINT") {
				return nil, fmt.Errorf("auto increment must use BIGINT primary key")
			}
		}
		for _, key := range append([][]string{t.PrimaryKey}, t.Unique...) {
			for _, name := range key {
				if _, ok := t.Field(name); !ok {
					return nil, fmt.Errorf("index references missing column %s", name)
				}
			}
		}
		tables = append(tables, t)
	}
	for _, t := range tables {
		for _, fk := range t.ForeignKeys {
			target, ok := findTable(tables, fk.Table)
			if !ok || len(fk.Columns) != len(fk.References) {
				return nil, fmt.Errorf("invalid foreign reference")
			}
			for i, name := range fk.Columns {
				a, ok := t.Field(name)
				b, bok := target.Field(fk.References[i])
				if !ok || !bok || a.Type != b.Type {
					return nil, fmt.Errorf("foreign column mismatch")
				}
			}
		}
	}
	return tables, nil
}
func findTable(tables []Table, name string) (Table, bool) {
	for _, t := range tables {
		if t.Name == name {
			return t, true
		}
	}
	return Table{}, false
}
func literalValue(expr ast.ExprNode, typ string) (mysqlv1.Value, error) {
	v, ok := expr.(ast.ValueExpr)
	if !ok {
		return mysqlv1.Value{}, fmt.Errorf("only typed literal expressions supported")
	}
	switch value := v.GetValue().(type) {
	case nil:
		return mysqlv1.Null(typ), nil
	case int64:
		if typ == "BIGINT" {
			return mysqlv1.Int(value), nil
		}
	case uint64:
		if typ == "BIGINT" && value <= math.MaxInt64 {
			return mysqlv1.Int(int64(value)), nil
		}
	case string:
		if typ == "VARCHAR" {
			return mysqlv1.Text(value), nil
		}
	}
	return mysqlv1.Value{}, fmt.Errorf("literal does not match %s", strings.ToUpper(typ))
}
