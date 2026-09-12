package main

import (
	"context"
	"encoding/json"
	"github.com/go-mysql-org/go-mysql/client"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestWireQueryPrepareAndDisconnect(t *testing.T) {
	p := &plugin{}
	waiting := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	dispatch := func(ctx context.Context, q pluginapi.Request) (json.RawMessage, error) {
		switch q.Operation {
		case "connection.close":
			closed <- struct{}{}
			return json.RawMessage(`{"kind":"closed"}`), nil
		case "connection.open", "database.use", "statement.close":
			return mysqlv1.Encode(mysqlv1.OK{Kind: "ok", AffectedRows: "0", LastInsertID: "0", Status: mysqlv1.SessionStatus{Autocommit: true}}), nil
		}
		var query mysqlv1.Query
		if err := json.Unmarshal(q.Payload, &query); err != nil {
			return nil, err
		}
		if query.SQL == "WAIT" {
			waiting <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return nil, ctx.Err()
		}
		columns := []mysqlv1.Column{{Name: "id", Type: "BIGINT"}}
		if q.Operation == "statement.prepare" {
			return mysqlv1.Encode(mysqlv1.Metadata{StatementID: "s1", Columns: columns, Parameters: columns}), nil
		}
		return mysqlv1.Encode(mysqlv1.Rows{Kind: "rows", Columns: columns, Rows: [][]json.RawMessage{{json.RawMessage(`"9007199254740993"`)}}, Status: mysqlv1.SessionStatus{Autocommit: true}}), nil
	}
	ready, err := p.Start(context.Background(), pluginapi.InstanceSpec{InstanceID: "wire", Config: json.RawMessage(`{"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}`)}, dispatch)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop(context.Background())
	c, err := client.Connect(ready.Endpoints["mysql"], "mock", "mock-local", "app")
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Execute("SELECT id")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := r.GetInt(0, 0)
	if n != 9007199254740993 {
		t.Fatal(n)
	}
	stmt, err := c.Prepare("SELECT id WHERE id = ?")
	if err != nil {
		t.Fatal(err)
	}
	r, err = stmt.Execute(int64(9007199254740993))
	if err != nil {
		t.Fatal(err)
	}
	n, _ = r.GetInt(0, 0)
	if n != 9007199254740993 {
		t.Fatal(n)
	}
	if err = stmt.Close(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { c.Execute("WAIT"); close(finished) }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("query not dispatched")
	}
	c.Conn.Conn.Close()
	<-finished
	c.Close()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect failed to cancel query")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("right not informed of disconnect")
	}
}
