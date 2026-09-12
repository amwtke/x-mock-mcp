package main

import (
	"context"
	"encoding/json"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func stateFixture(t *testing.T) (*state, []Table) {
	t.Helper()
	tables, err := ParseDDL(testDDL)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newState(DatabaseScenario{Tables: tables, Initial: map[string][]Entity{"products": {{"id": mysqlv1.Int(1001), "name": mysqlv1.Text("键盘"), "price_cents": mysqlv1.Int(9900)}}, "cart_items": {}}, NextIDs: map[string]string{"cart_items": "5001"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, tables
}
func plan(t *testing.T, tables []Table, sql string, n int) *Plan {
	t.Helper()
	params := make([]Parameter, n)
	for i := range params {
		params[i] = Parameter{Name: "p", Type: "BIGINT"}
	}
	p, err := Compile(Statement{SQL: sql, Parameters: params}, tables, "app")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func rowQuantity(t *testing.T, s *state, tables []Table, connection string, user int64) (int, int64) {
	t.Helper()
	p := plan(t, tables, "SELECT id, quantity FROM cart_items WHERE user_id = ?", 1)
	raw, err := s.execute(context.Background(), connection, p, []mysqlv1.Value{mysqlv1.Int(user)})
	if err != nil {
		t.Fatal(err)
	}
	var result mysqlv1.Rows
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) == 0 {
		return 0, 0
	}
	value := mysqlv1.Value{Type: "BIGINT", Value: result.Rows[0][1]}
	n, err := value.Int64()
	if err != nil {
		t.Fatal(err)
	}
	return len(result.Rows), n
}
func TestStateInsertUpdateAndUserIsolation(t *testing.T) {
	s, tables := stateFixture(t)
	ctx := context.Background()
	if count, _ := rowQuantity(t, s, tables, "a", 2001); count != 0 {
		t.Fatal("initial cart not empty")
	}
	insert := plan(t, tables, "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)", 3)
	values := []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(1)}
	raw, err := s.execute(ctx, "a", insert, values)
	if err != nil {
		t.Fatal(err)
	}
	var ok mysqlv1.OK
	json.Unmarshal(raw, &ok)
	if ok.AffectedRows != "1" || ok.LastInsertID != "5001" {
		t.Fatal(ok)
	}
	update := plan(t, tables, "UPDATE cart_items SET quantity = quantity + ? WHERE user_id = ? AND product_id = ?", 3)
	if _, err = s.execute(ctx, "a", update, []mysqlv1.Value{mysqlv1.Int(1), mysqlv1.Int(2001), mysqlv1.Int(1001)}); err != nil {
		t.Fatal(err)
	}
	if count, n := rowQuantity(t, s, tables, "b", 2001); count != 1 || n != 2 {
		t.Fatal(count, n)
	}
	if count, _ := rowQuantity(t, s, tables, "b", 2002); count != 0 {
		t.Fatal("user isolation lost")
	}
	if _, err = s.execute(ctx, "a", insert, values); err == nil {
		t.Fatal("duplicate unique cart accepted")
	}
	if count, n := rowQuantity(t, s, tables, "a", 2001); count != 1 || n != 2 {
		t.Fatal("failed write changed state")
	}
}
func TestTransactionCommitRollbackAndConflict(t *testing.T) {
	s, tables := stateFixture(t)
	ctx := context.Background()
	command := func(conn, sql string) {
		t.Helper()
		if _, known, err := s.control(ctx, conn, sql); !known || err != nil {
			t.Fatal(sql, known, err)
		}
	}
	insert := plan(t, tables, "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)", 3)
	command("a", "SET autocommit = 0")
	if _, err := s.execute(ctx, "a", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(1)}); err != nil {
		t.Fatal(err)
	}
	if count, _ := rowQuantity(t, s, tables, "a", 2001); count != 1 {
		t.Fatal("cannot read own write")
	}
	if count, _ := rowQuantity(t, s, tables, "b", 2001); count != 0 {
		t.Fatal("uncommitted write leaked")
	}
	command("a", "ROLLBACK")
	if count, _ := rowQuantity(t, s, tables, "b", 2001); count != 0 {
		t.Fatal("rollback leaked")
	}
	if _, err := s.execute(ctx, "a", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(1)}); err != nil {
		t.Fatal(err)
	}
	command("a", "COMMIT")
	if count, _ := rowQuantity(t, s, tables, "b", 2001); count != 1 {
		t.Fatal("commit invisible")
	}
	command("b", "SET autocommit=0")
	update := plan(t, tables, "UPDATE cart_items SET quantity = quantity + ? WHERE user_id = ?", 2)
	for _, conn := range []string{"a", "b"} {
		if _, err := s.execute(ctx, conn, update, []mysqlv1.Value{mysqlv1.Int(1), mysqlv1.Int(2001)}); err != nil {
			t.Fatal(err)
		}
	}
	command("a", "COMMIT")
	if _, _, err := s.control(ctx, "b", "COMMIT"); err == nil {
		t.Fatal("conflicting commit lost an update")
	}
	if count, n := rowQuantity(t, s, tables, "c", 2001); count != 1 || n != 2 {
		t.Fatal(count, n)
	}
}

func TestStateRejectsInvalidWritesAtomicallyAndHonorsFoundRows(t *testing.T) {
	s, tables := stateFixture(t)
	ctx := context.Background()
	insert := plan(t, tables, "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)", 3)
	if _, err := s.execute(ctx, "a", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(9999), mysqlv1.Int(1)}); err == nil {
		t.Fatal("missing foreign key accepted")
	}
	if n, _ := rowQuantity(t, s, tables, "a", 2001); n != 0 {
		t.Fatal("failed FK mutated state")
	}
	if _, err := s.execute(ctx, "a", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(9223372036854775807)}); err != nil {
		t.Fatal(err)
	}
	increment := plan(t, tables, "UPDATE cart_items SET quantity = quantity + ? WHERE user_id = ?", 2)
	if _, err := s.execute(ctx, "a", increment, []mysqlv1.Value{mysqlv1.Int(1), mysqlv1.Int(2001)}); err == nil {
		t.Fatal("overflow accepted")
	}
	if _, n := rowQuantity(t, s, tables, "a", 2001); n != 9223372036854775807 {
		t.Fatal("overflow changed row")
	}
	noop := plan(t, tables, "UPDATE cart_items SET quantity = ? WHERE user_id = ?", 2)
	for _, found := range []bool{false, true} {
		s.mu.Lock()
		s.conn("a").foundRows = found
		s.mu.Unlock()
		raw, err := s.execute(ctx, "a", noop, []mysqlv1.Value{mysqlv1.Int(9223372036854775807), mysqlv1.Int(2001)})
		if err != nil {
			t.Fatal(err)
		}
		var r mysqlv1.OK
		json.Unmarshal(raw, &r)
		want := "0"
		if found {
			want = "1"
		}
		if r.AffectedRows != want {
			t.Fatal(found, r)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.execute(cancelled, "a", noop, []mysqlv1.Value{mysqlv1.Int(1), mysqlv1.Int(2001)}); err == nil {
		t.Fatal("cancelled write applied")
	}
	if _, n := rowQuantity(t, s, tables, "a", 2001); n != 9223372036854775807 {
		t.Fatal("cancelled write changed row")
	}
}
func TestTransactionDisconnectDiscardsWorkingSet(t *testing.T) {
	s, tables := stateFixture(t)
	ctx := context.Background()
	s.control(ctx, "a", "BEGIN")
	insert := plan(t, tables, "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)", 3)
	if _, err := s.execute(ctx, "a", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(1)}); err != nil {
		t.Fatal(err)
	}
	s.closeConnection("a")
	if n, _ := rowQuantity(t, s, tables, "b", 2001); n != 0 {
		t.Fatal("disconnected transaction leaked")
	}
}
