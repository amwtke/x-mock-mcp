package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

type mysqlFailure struct {
	Number            uint16
	SQLState, Message string
}

func (e *mysqlFailure) Error() string { return e.Message }
func dbError(number uint16, sqlstate, message string) error {
	return &mysqlFailure{number, sqlstate, message}
}

type recordVersion struct {
	// A nil row is a deletion tombstone. Retain its version so a pending
	// transaction cannot overwrite a concurrent delete or delete/reinsert.
	row     Entity
	version uint64
}
type connection struct {
	autocommit, active, foundRows, readOnly bool
	lastInsert                              uint64
	writes                                  map[string]map[string]Entity
	base                                    map[string]uint64
	statements                              map[string]Statement
	nextStatement                           uint64
	settings                                map[string]string
}
type state struct {
	mu        sync.Mutex
	tables    []Table
	committed map[string]map[string]recordVersion
	sessions  map[string]*connection
	nextIDs   map[string]int64
	version   uint64
}

func newState(db DatabaseScenario) (*state, error) {
	if err := validateInitial(db.Tables, db.Initial); err != nil {
		return nil, err
	}
	s := &state{tables: db.Tables, committed: map[string]map[string]recordVersion{}, sessions: map[string]*connection{}, nextIDs: map[string]int64{}}
	for _, table := range db.Tables {
		s.committed[table.Name] = map[string]recordVersion{}
		next := int64(1)
		for _, row := range db.Initial[table.Name] {
			key, _ := row[table.PrimaryKey[0]].String()
			s.committed[table.Name][key] = recordVersion{cloneRow(row), 0}
			for _, f := range table.Columns {
				if f.AutoIncrement {
					n, _ := row[f.Name].Int64()
					if n == math.MaxInt64 {
						return nil, fmt.Errorf("auto increment exhausted")
					}
					if n >= next {
						next = n + 1
					}
				}
			}
		}
		if v, ok := db.NextIDs[table.Name]; ok {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < next {
				return nil, fmt.Errorf("invalid auto increment seed")
			}
			next = n
		}
		s.nextIDs[table.Name] = next
	}
	return s, nil
}
func cloneRow(row Entity) Entity {
	if row == nil {
		return nil
	}
	out := Entity{}
	for k, v := range row {
		if v.IsNull() {
			out[k] = mysqlv1.Null(v.Type)
		} else {
			str, _ := v.String()
			v.Value = mysqlv1.Encode(str)
			out[k] = v
		}
	}
	return out
}
func (s *state) conn(id string) *connection {
	c := s.sessions[id]
	if c == nil {
		c = &connection{autocommit: true, writes: map[string]map[string]Entity{}, base: map[string]uint64{}, statements: map[string]Statement{}, settings: map[string]string{}}
		s.sessions[id] = c
	}
	return c
}
func status(c *connection) mysqlv1.SessionStatus {
	return mysqlv1.SessionStatus{Autocommit: c.autocommit, InTransaction: c.active}
}
func okResult(c *connection, affected, insert uint64) json.RawMessage {
	return mysqlv1.Encode(mysqlv1.OK{Kind: "ok", AffectedRows: strconv.FormatUint(affected, 10), LastInsertID: strconv.FormatUint(insert, 10), Status: status(c)})
}
func (s *state) view(c *connection) map[string]map[string]Entity {
	view := map[string]map[string]Entity{}
	for table, rows := range s.committed {
		view[table] = map[string]Entity{}
		for k, r := range rows {
			if r.row != nil {
				view[table][k] = cloneRow(r.row)
			}
		}
	}
	if c != nil {
		for table, rows := range c.writes {
			for key, row := range rows {
				if row == nil {
					delete(view[table], key)
				} else {
					view[table][key] = cloneRow(row)
				}
			}
		}
	}
	return view
}
func asInitial(view map[string]map[string]Entity) map[string][]Entity {
	out := map[string][]Entity{}
	for table, rows := range view {
		keys := make([]string, 0, len(rows))
		for k := range rows {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[table] = append(out[table], cloneRow(rows[k]))
		}
	}
	return out
}
func operandValue(op Operand, params []mysqlv1.Value) (mysqlv1.Value, error) {
	if op.Parameter != nil {
		if *op.Parameter < 0 || *op.Parameter >= len(params) {
			return mysqlv1.Value{}, fmt.Errorf("parameter missing")
		}
		return params[*op.Parameter], nil
	}
	if op.Literal != nil {
		return *op.Literal, nil
	}
	return mysqlv1.Value{}, fmt.Errorf("operand has no value")
}
func equalValue(a, b mysqlv1.Value) bool {
	if a.Type != b.Type || a.IsNull() != b.IsNull() {
		return false
	}
	if a.IsNull() {
		return true
	}
	x, e := a.String()
	y, f := b.String()
	return e == nil && f == nil && x == y
}
func rowMatches(row Entity, predicates []Predicate, params []mysqlv1.Value) (bool, error) {
	for _, p := range predicates {
		value, err := operandValue(p.Value, params)
		if err != nil {
			return false, err
		}
		if value.IsNull() || row[p.Column].IsNull() || !equalValue(row[p.Column], value) {
			return false, nil
		}
	}
	return true, nil
}
func (s *state) execute(ctx context.Context, id string, p *Plan, params []mysqlv1.Value, rule ...string) (raw json.RawMessage, executionErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if executionErr == nil {
			statement := ""
			if len(rule) > 0 {
				statement = rule[0]
			}
			phase := "autocommit"
			if c := s.sessions[id]; c != nil && c.active {
				phase = "transaction"
			}
			raw = executionEvidence(raw, mysqlv1.Execution{StatementID: statement, StateVersion: s.version, Phase: phase, Source: "stateful"})
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(params) != len(p.Parameters) {
		return nil, dbError(1210, "HY000", "incorrect parameter count")
	}
	for i, value := range params {
		if err := mysqlv1.ValidateValue(p.Parameters[i], value); err != nil {
			return nil, dbError(1210, "HY000", err.Error())
		}
	}
	c := s.conn(id)
	if !c.autocommit {
		c.active = true
	}
	view := s.view(c)
	if p.Kind == "select" {
		rows := []Entity{}
		for _, row := range view[p.Table] {
			match, err := rowMatches(row, p.Predicates, params)
			if err != nil {
				return nil, err
			}
			if match {
				rows = append(rows, row)
			}
		}
		table, _ := findTable(s.tables, p.Table)
		sort.Slice(rows, func(i, j int) bool {
			orders := p.Order
			if len(orders) == 0 {
				orders = []Order{{Column: table.PrimaryKey[0]}}
			}
			for _, order := range orders {
				a, b := rows[i][order.Column], rows[j][order.Column]
				cmp := compareValue(a, b)
				if cmp != 0 {
					if order.Descending {
						return cmp > 0
					}
					return cmp < 0
				}
			}
			return false
		})
		if p.Limit != nil && uint64(len(rows)) > *p.Limit {
			rows = rows[:*p.Limit]
		}
		result := mysqlv1.Rows{Kind: "rows", Columns: p.Columns, Rows: [][]json.RawMessage{}, Status: status(c)}
		for _, row := range rows {
			values := []json.RawMessage{}
			for _, name := range p.Projection {
				values = append(values, row[name].Value)
			}
			result.Rows = append(result.Rows, values)
		}
		if err := mysqlv1.ValidateRows(mysqlv1.Metadata{Columns: p.Columns}, result); err != nil {
			return nil, err
		}
		return mysqlv1.Encode(result), nil
	}
	if c.readOnly {
		return nil, dbError(1792, "25006", "write in read-only transaction")
	}
	table, ok := findTable(s.tables, p.Table)
	if !ok {
		return nil, dbError(1146, "42S02", "unknown table")
	}
	changes := map[string]Entity{}
	var matched, changed, insertID uint64
	switch p.Kind {
	case "insert":
		row := Entity{}
		for _, field := range table.Columns {
			if field.Default != nil {
				row[field.Name] = *field.Default
			} else if field.Nullable {
				row[field.Name] = mysqlv1.Null(field.Type)
			}
		}
		for _, a := range p.Assignments {
			value, err := operandValue(a.Value, params)
			if err != nil {
				return nil, err
			}
			row[a.Column] = value
		}
		for _, field := range table.Columns {
			if field.AutoIncrement {
				v, exists := row[field.Name]
				if !exists || v.IsNull() {
					n := s.nextIDs[table.Name]
					if n <= 0 || n == math.MaxInt64 {
						return nil, dbError(1467, "HY000", "auto increment exhausted")
					}
					s.nextIDs[table.Name]++
					row[field.Name] = mysqlv1.Int(n)
					insertID = uint64(n)
				} else {
					n, err := v.Int64()
					if err != nil || n <= 0 {
						return nil, dbError(1235, "42000", "only positive explicit auto increment IDs supported")
					}
					if n >= s.nextIDs[table.Name] {
						if n == math.MaxInt64 {
							return nil, dbError(1467, "HY000", "auto increment exhausted")
						}
						s.nextIDs[table.Name] = n + 1
					}
				}
			}
		}
		key, err := row[table.PrimaryKey[0]].String()
		if err != nil {
			return nil, dbError(1364, "HY000", "primary key missing")
		}
		if _, exists := view[table.Name][key]; exists {
			return nil, dbError(1062, "23000", "duplicate primary key")
		}
		changes[key] = cloneRow(row)
		matched = 1
		changed = 1
	case "update":
		for key, existing := range view[table.Name] {
			match, err := rowMatches(existing, p.Predicates, params)
			if err != nil {
				return nil, err
			}
			if !match {
				continue
			}
			matched++
			row := cloneRow(existing)
			different := false
			for _, a := range p.Assignments {
				value, err := operandValue(a.Value, params)
				if err != nil {
					return nil, err
				}
				if a.AddFrom != "" {
					base, err := row[a.AddFrom].Int64()
					if err != nil {
						return nil, err
					}
					delta, err := value.Int64()
					if err != nil {
						return nil, err
					}
					if (delta > 0 && base > math.MaxInt64-delta) || (delta < 0 && base < math.MinInt64-delta) {
						return nil, dbError(1690, "22003", "BIGINT overflow")
					}
					value = mysqlv1.Int(base + delta)
				}
				different = different || !equalValue(row[a.Column], value)
				row[a.Column] = value
			}
			if different {
				changes[key] = cloneRow(row)
				changed++
			}
		}
	case "delete":
		for key, row := range view[table.Name] {
			match, err := rowMatches(row, p.Predicates, params)
			if err != nil {
				return nil, err
			}
			if match {
				changes[key] = nil
				changed++
			}
		}
	default:
		return nil, dbError(1235, "42000", "unsupported state operation")
	}
	for key, row := range changes {
		if row == nil {
			delete(view[table.Name], key)
		} else {
			view[table.Name][key] = row
		}
	}
	if err := validateInitial(s.tables, asInitial(view)); err != nil {
		if p.Kind == "delete" && strings.Contains(err.Error(), "foreign key") {
			return nil, dbError(1451, "23000", err.Error())
		}
		return nil, constraintError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.active {
		if c.writes[table.Name] == nil {
			c.writes[table.Name] = map[string]Entity{}
		}
		for key, row := range changes {
			identity := table.Name + "\x00" + key
			if _, exists := c.base[identity]; !exists {
				c.base[identity] = s.committed[table.Name][key].version
			}
			c.writes[table.Name][key] = row
		}
	} else {
		s.version++
		for key, row := range changes {
			s.committed[table.Name][key] = recordVersion{row, s.version}
		}
	}
	if insertID != 0 {
		c.lastInsert = insertID
	}
	if c.foundRows && p.Kind == "update" {
		changed = matched
	}
	return okResult(c, changed, insertID), nil
}
func compareValue(a, b mysqlv1.Value) int {
	if a.IsNull() {
		if b.IsNull() {
			return 0
		}
		return -1
	}
	if b.IsNull() {
		return 1
	}
	if a.Type == "BIGINT" {
		x, _ := a.Int64()
		y, _ := b.Int64()
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	}
	x, _ := a.String()
	y, _ := b.String()
	return strings.Compare(x, y)
}
func constraintError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "duplicate key"):
		return dbError(1062, "23000", message)
	case strings.Contains(message, "foreign key"):
		return dbError(1452, "23000", message)
	case strings.Contains(message, "VARCHAR too long"):
		return dbError(1406, "22001", message)
	default:
		return dbError(1048, "23000", message)
	}
}
