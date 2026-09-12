package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func insertedCart(t *testing.T) (*state, []Table) {
	t.Helper()
	s, tables := stateFixture(t)
	insert := plan(t, tables, "INSERT INTO cart_items (user_id,product_id,quantity) VALUES (?,?,?)", 3)
	if _, err := s.execute(context.Background(), "seed", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(1)}); err != nil {
		t.Fatal(err)
	}
	return s, tables
}
func deleteCount(t *testing.T, s *state, tables []Table, connection string, want string) {
	t.Helper()
	p := plan(t, tables, "DELETE FROM cart_items WHERE id=? AND user_id=?", 2)
	raw, err := s.execute(context.Background(), connection, p, []mysqlv1.Value{mysqlv1.Int(5001), mysqlv1.Int(2001)})
	if err != nil {
		t.Fatal(err)
	}
	var result mysqlv1.OK
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.AffectedRows != want {
		t.Fatalf("delete affected=%s want=%s", result.AffectedRows, want)
	}
}
func TestDeleteChangesRowsAndCounts(t *testing.T) {
	s, tables := insertedCart(t)
	deleteCount(t, s, tables, "a", "1")
	deleteCount(t, s, tables, "a", "0")
	if count, _ := rowQuantity(t, s, tables, "b", 2001); count != 0 {
		t.Fatal("delete did not remove row")
	}
}
func TestDeleteTransactionCommitRollbackDisconnect(t *testing.T) {
	for _, end := range []string{"COMMIT", "ROLLBACK", "disconnect"} {
		t.Run(end, func(t *testing.T) {
			s, tables := insertedCart(t)
			ctx := context.Background()
			if _, _, err := s.control(ctx, "a", "BEGIN"); err != nil {
				t.Fatal(err)
			}
			deleteCount(t, s, tables, "a", "1")
			if count, _ := rowQuantity(t, s, tables, "a", 2001); count != 0 {
				t.Fatal("own deleted row visible")
			}
			if count, _ := rowQuantity(t, s, tables, "b", 2001); count != 1 {
				t.Fatal("uncommitted delete leaked")
			}
			if end == "disconnect" {
				s.closeConnection("a")
			} else if _, _, err := s.control(ctx, "a", end); err != nil {
				t.Fatal(err)
			}
			want := 1
			if end == "COMMIT" {
				want = 0
			}
			if count, _ := rowQuantity(t, s, tables, "b", 2001); count != want {
				t.Fatal("transaction delete visibility", count, want)
			}
		})
	}
}
func TestDeleteConflictAndForeignKeyRestriction(t *testing.T) {
	s, tables := insertedCart(t)
	ctx := context.Background()
	parent := plan(t, tables, "DELETE FROM products WHERE id=?", 1)
	_, err := s.execute(ctx, "a", parent, []mysqlv1.Value{mysqlv1.Int(1001)})
	var failure *mysqlFailure
	if !errors.As(err, &failure) || failure.Number != 1451 || failure.SQLState != "23000" {
		t.Fatal("parent delete should be restricted", err)
	}
	if count, _ := rowQuantity(t, s, tables, "a", 2001); count != 1 {
		t.Fatal("failed delete changed child")
	}
	if _, _, err = s.control(ctx, "a", "BEGIN"); err != nil {
		t.Fatal(err)
	}
	deleteCount(t, s, tables, "a", "1")
	update := plan(t, tables, "UPDATE cart_items SET quantity=? WHERE id=?", 2)
	if _, err = s.execute(ctx, "b", update, []mysqlv1.Value{mysqlv1.Int(2), mysqlv1.Int(5001)}); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.control(ctx, "a", "COMMIT")
	if !errors.As(err, &failure) || failure.Number != 1213 {
		t.Fatal("delete lost concurrent update", err)
	}
	if count, qty := rowQuantity(t, s, tables, "b", 2001); count != 1 || qty != 2 {
		t.Fatal("conflict changed committed row", count, qty)
	}
}
func TestDeleteRejectsUnboundedAndUnsupportedSQL(t *testing.T) {
	_, tables := stateFixture(t)
	for _, sql := range []string{"DELETE FROM cart_items", "DELETE FROM cart_items WHERE id>1", "DELETE FROM cart_items WHERE id=1 LIMIT 1", "DELETE FROM cart_items WHERE id=1 ORDER BY id", "DELETE FROM cart_items PARTITION (p0) WHERE id=1", "DELETE LOW_PRIORITY FROM cart_items WHERE id=1", "DELETE a FROM cart_items a JOIN products p ON a.product_id=p.id WHERE a.id=1"} {
		if _, err := Compile(Statement{SQL: sql}, tables, "app"); err == nil {
			t.Fatal("unsupported delete accepted", sql)
		}
	}
}

func TestConcurrentDeleteCannotBeResurrectedByPendingUpdate(t *testing.T) {
	for _, reinsert := range []bool{false, true} {
		s, tables := insertedCart(t)
		ctx := context.Background()
		if _, _, err := s.control(ctx, "a", "BEGIN"); err != nil {
			t.Fatal(err)
		}
		update := plan(t, tables, "UPDATE cart_items SET quantity=? WHERE id=?", 2)
		if _, err := s.execute(ctx, "a", update, []mysqlv1.Value{mysqlv1.Int(2), mysqlv1.Int(5001)}); err != nil {
			t.Fatal(err)
		}
		deleteCount(t, s, tables, "b", "1")
		if reinsert {
			insert := plan(t, tables, "INSERT INTO cart_items (id,user_id,product_id,quantity) VALUES (?,?,?,?)", 4)
			if _, err := s.execute(ctx, "b", insert, []mysqlv1.Value{mysqlv1.Int(5001), mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(3)}); err != nil {
				t.Fatal(err)
			}
		}
		_, _, err := s.control(ctx, "a", "COMMIT")
		var failure *mysqlFailure
		if !errors.As(err, &failure) || failure.Number != 1213 {
			t.Fatal("pending update lost delete version", err)
		}
		count, qty := rowQuantity(t, s, tables, "b", 2001)
		if (!reinsert && count != 0) || (reinsert && (count != 1 || qty != 3)) {
			t.Fatal("conflict resurrected/overwrote row", count, qty)
		}
	}
}
