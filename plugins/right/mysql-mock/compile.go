package main

import (
	"fmt"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/opcode"
	"github.com/pingcap/tidb/pkg/parser/test_driver"
	"sort"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

type markers struct {
	nodes []*test_driver.ParamMarkerExpr
}

func (v *markers) Enter(n ast.Node) (ast.Node, bool) {
	if p, ok := n.(*test_driver.ParamMarkerExpr); ok {
		v.nodes = append(v.nodes, p)
	}
	return n, false
}
func (v *markers) Leave(n ast.Node) (ast.Node, bool) { return n, true }

type compiler struct {
	table      Table
	database   string
	parameters []Parameter
	indexes    map[*test_driver.ParamMarkerExpr]int
	metadata   []mysqlv1.Column
}

func Compile(statement Statement, tables []Table, database string) (*Plan, error) {
	stmts, warnings, err := parser.New().ParseSQL(statement.SQL)
	if err != nil {
		return nil, err
	}
	if len(stmts) != 1 || len(warnings) > 0 {
		return nil, fmt.Errorf("exactly one supported SQL statement required")
	}
	visitor := &markers{}
	stmts[0].Accept(visitor)
	sort.Slice(visitor.nodes, func(i, j int) bool { return visitor.nodes[i].Offset < visitor.nodes[j].Offset })
	if len(visitor.nodes) != len(statement.Parameters) {
		return nil, fmt.Errorf("parameter count mismatch")
	}
	c := compiler{database: database, parameters: statement.Parameters, indexes: map[*test_driver.ParamMarkerExpr]int{}, metadata: make([]mysqlv1.Column, len(statement.Parameters))}
	for i, n := range visitor.nodes {
		c.indexes[n] = i
	}
	p := &Plan{Columns: []mysqlv1.Column{}, Parameters: []mysqlv1.Column{}}
	getTable := func(ref *ast.TableRefsClause) error {
		if ref == nil || ref.TableRefs == nil || ref.TableRefs.Right != nil || ref.TableRefs.On != nil {
			return fmt.Errorf("only single-table SQL is supported")
		}
		source, ok := ref.TableRefs.Left.(*ast.TableSource)
		if !ok || source.AsName.O != "" {
			return fmt.Errorf("table aliases/subqueries unsupported")
		}
		name, ok := source.Source.(*ast.TableName)
		if !ok {
			return fmt.Errorf("table reference unsupported")
		}
		if len(name.PartitionNames) > 0 || len(name.IndexHints) > 0 {
			return fmt.Errorf("table partitions and index hints unsupported")
		}
		if name.Schema.O != "" && name.Schema.O != database {
			return fmt.Errorf("database qualifier mismatch")
		}
		t, ok := findTable(tables, name.Name.O)
		if !ok {
			return fmt.Errorf("unknown table %s", name.Name.O)
		}
		c.table = t
		p.Table = t.Name
		return nil
	}
	switch n := stmts[0].(type) {
	case *ast.SelectStmt:
		if n.Distinct || n.GroupBy != nil || n.Having != nil || n.LockInfo != nil || n.With != nil || n.SelectIntoOpt != nil || len(n.WindowSpecs) > 0 || len(n.TableHints) > 0 {
			return nil, fmt.Errorf("unsupported SELECT clause")
		}
		if err = getTable(n.From); err != nil {
			return nil, err
		}
		p.Kind = "select"
		for _, item := range n.Fields.Fields {
			if item.WildCard != nil {
				return nil, fmt.Errorf("explicit result columns required")
			}
			name, err := c.column(item.Expr)
			if err != nil {
				return nil, err
			}
			field, _ := c.table.Field(name)
			column := field.Column
			if item.AsName.O != "" {
				column.Name = item.AsName.O
			}
			p.Projection = append(p.Projection, name)
			p.Columns = append(p.Columns, column)
		}
		p.Predicates, err = c.predicates(n.Where)
		if err != nil {
			return nil, err
		}
		if n.OrderBy != nil {
			for _, item := range n.OrderBy.Items {
				name, err := c.column(item.Expr)
				if err != nil {
					return nil, err
				}
				p.Order = append(p.Order, Order{Column: name, Descending: item.Desc})
			}
		}
		if n.Limit != nil {
			if n.Limit.Offset != nil {
				offset, err := literalValue(n.Limit.Offset, "BIGINT")
				if err != nil {
					return nil, err
				}
				v, _ := offset.Int64()
				if v != 0 {
					return nil, fmt.Errorf("nonzero LIMIT offset unsupported")
				}
			}
			value, err := literalValue(n.Limit.Count, "BIGINT")
			if err != nil {
				return nil, err
			}
			v, err := value.Int64()
			if err != nil || v < 0 || v > 1000 {
				return nil, fmt.Errorf("invalid LIMIT")
			}
			limit := uint64(v)
			p.Limit = &limit
		}
	case *ast.InsertStmt:
		if n.IsReplace || n.IgnoreErr || n.Select != nil || n.Setlist || len(n.Lists) != 1 || len(n.OnDuplicate) > 0 || len(n.PartitionNames) > 0 || len(n.TableHints) > 0 {
			return nil, fmt.Errorf("only single-row INSERT VALUES supported")
		}
		if err = getTable(n.Table); err != nil {
			return nil, err
		}
		p.Kind = "insert"
		if len(n.Columns) == 0 || len(n.Columns) != len(n.Lists[0]) {
			return nil, fmt.Errorf("explicit insert columns required")
		}
		seen := map[string]bool{}
		for i, col := range n.Columns {
			name, err := c.columnName(col)
			if err != nil {
				return nil, err
			}
			if seen[name] {
				return nil, fmt.Errorf("duplicate insert column")
			}
			seen[name] = true
			field, _ := c.table.Field(name)
			value, err := c.operand(n.Lists[0][i], field.Column)
			if err != nil {
				return nil, err
			}
			p.Assignments = append(p.Assignments, Assignment{Column: name, Value: value})
		}
	case *ast.UpdateStmt:
		if n.MultipleTable || n.IgnoreErr || n.Where == nil || n.Limit != nil || n.Order != nil || n.With != nil || len(n.TableHints) > 0 {
			return nil, fmt.Errorf("unsupported or unbounded UPDATE")
		}
		if err = getTable(n.TableRefs); err != nil {
			return nil, err
		}
		p.Kind = "update"
		seen := map[string]bool{}
		for _, assignment := range n.List {
			name, err := c.columnName(assignment.Column)
			if err != nil {
				return nil, err
			}
			if seen[name] {
				return nil, fmt.Errorf("duplicate update target")
			}
			seen[name] = true
			if name == c.table.PrimaryKey[0] {
				return nil, fmt.Errorf("primary key update unsupported")
			}
			field, _ := c.table.Field(name)
			a := Assignment{Column: name}
			expr := assignment.Expr
			if binary, ok := expr.(*ast.BinaryOperationExpr); ok {
				if binary.Op != opcode.Plus || field.Type != "BIGINT" {
					return nil, fmt.Errorf("only BIGINT addition supported")
				}
				from, err := c.column(binary.L)
				if err != nil {
					return nil, err
				}
				if from != name {
					return nil, fmt.Errorf("cross-column arithmetic unsupported")
				}
				a.AddFrom = from
				expr = binary.R
			}
			a.Value, err = c.operand(expr, field.Column)
			if err != nil {
				return nil, err
			}
			p.Assignments = append(p.Assignments, a)
		}
		p.Predicates, err = c.predicates(n.Where)
		if err != nil {
			return nil, err
		}
	case *ast.DeleteStmt:
		if n.IsMultiTable || n.Where == nil || n.Order != nil || n.Limit != nil || n.IgnoreErr || n.Quick || n.With != nil || len(n.TableHints) > 0 || n.Priority != 0 {
			return nil, fmt.Errorf("only bounded single-table DELETE supported")
		}
		if err = getTable(n.TableRefs); err != nil {
			return nil, err
		}
		p.Kind = "delete"
		p.Predicates, err = c.predicates(n.Where)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("SQL kind is unsupported")
	}
	for i, metadata := range c.metadata {
		if metadata.Name == "" {
			return nil, fmt.Errorf("unbound parameter %d", i)
		}
	}
	p.Parameters = c.metadata
	return p, nil
}
func (c *compiler) columnName(n *ast.ColumnName) (string, error) {
	if n.Schema.O != "" && n.Schema.O != c.database {
		return "", fmt.Errorf("column database mismatch")
	}
	if n.Table.O != "" && n.Table.O != c.table.Name {
		return "", fmt.Errorf("column table mismatch")
	}
	if _, ok := c.table.Field(n.Name.O); !ok {
		return "", fmt.Errorf("unknown column %s", n.Name.O)
	}
	return n.Name.O, nil
}
func (c *compiler) column(expr ast.ExprNode) (string, error) {
	n, ok := expr.(*ast.ColumnNameExpr)
	if !ok {
		return "", fmt.Errorf("column expression unsupported")
	}
	return c.columnName(n.Name)
}
func (c *compiler) operand(expr ast.ExprNode, column mysqlv1.Column) (Operand, error) {
	if marker, ok := expr.(*test_driver.ParamMarkerExpr); ok {
		index := c.indexes[marker]
		decl := c.parameters[index]
		if decl.Type != column.Type {
			return Operand{}, fmt.Errorf("parameter %s type differs from DDL", decl.Name)
		}
		c.metadata[index] = mysqlv1.Column{Name: decl.Name, Type: decl.Type, Nullable: decl.Nullable}
		for _, allowed := range decl.Allowed {
			if err := mysqlv1.ValidateValue(c.metadata[index], allowed); err != nil {
				return Operand{}, err
			}
		}
		return Operand{Parameter: &index}, nil
	}
	value, err := literalValue(expr, column.Type)
	if err != nil {
		return Operand{}, err
	}
	if err = mysqlv1.ValidateValue(column, value); err != nil {
		return Operand{}, err
	}
	return Operand{Literal: &value}, nil
}
func (c *compiler) predicates(expr ast.ExprNode) ([]Predicate, error) {
	if expr == nil {
		return nil, nil
	}
	if p, ok := expr.(*ast.ParenthesesExpr); ok {
		return c.predicates(p.Expr)
	}
	binary, ok := expr.(*ast.BinaryOperationExpr)
	if !ok {
		return nil, fmt.Errorf("only equality/AND predicates supported")
	}
	if binary.Op == opcode.LogicAnd {
		left, err := c.predicates(binary.L)
		if err != nil {
			return nil, err
		}
		right, err := c.predicates(binary.R)
		return append(left, right...), err
	}
	if binary.Op != opcode.EQ {
		return nil, fmt.Errorf("unsupported WHERE operator")
	}
	name, err := c.column(binary.L)
	if err != nil {
		return nil, err
	}
	column, _ := c.table.Field(name)
	value, err := c.operand(binary.R, column.Column)
	if err != nil {
		return nil, err
	}
	return []Predicate{{Column: name, Value: value}}, nil
}
