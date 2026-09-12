package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func runtimeFixture(t *testing.T, generation bool) *plugin {
	t.Helper()
	spec, _, body := preparationFixture(t)
	if generation {
		body.DatabaseScenario.Initial["products"] = []Entity{}
		body.DatabaseScenario.GenerationSlots = []Slot{{ID: "p1", Table: "products", Keys: []string{"1001"}, Fixed: map[string]mysqlv1.Value{"id": mysqlv1.Int(1001), "name": mysqlv1.Text("键盘"), "price_cents": mysqlv1.Int(9900)}}}
		spec.Candidate = mysqlv1.Encode(body)
	}
	p := &plugin{}
	report, err := p.Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatal(err, report)
	}
	_, err = p.Start(context.Background(), pluginapi.InstanceSpec{InstanceID: "right", Scenario: report.CompiledBody, Config: json.RawMessage(`{"mode":"AgentFill"}`)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(context.Background()) })
	return p
}
func request(operation, sql string, params ...mysqlv1.Value) pluginapi.Request {
	return pluginapi.Request{ID: "request", ConnectionID: "connection", Operation: operation, DeadlineUnixMS: time.Now().Add(time.Second).UnixMilli(), Payload: mysqlv1.Encode(mysqlv1.Query{Database: "app", SQL: sql, Params: params})}
}
func TestScenarioMatchingAndPreparedMetadata(t *testing.T) {
	p := runtimeFixture(t, false)
	ctx := context.Background()
	d, err := p.Execute(ctx, request("statement.prepare", "SELECT id, name, price_cents FROM products WHERE id = ?"))
	if err != nil || d.Kind != "completed" {
		t.Fatal(d, err)
	}
	var meta mysqlv1.Metadata
	json.Unmarshal(d.Payload, &meta)
	if len(meta.Columns) != 3 || meta.Columns[2].Type != "BIGINT" || len(meta.Parameters) != 1 {
		t.Fatal(meta)
	}
	d, err = p.Execute(ctx, request("query", "SELECT id, name, price_cents FROM products WHERE id = 1001"))
	if err != nil || d.Kind != "completed" {
		t.Fatal(d, err)
	}
	var rows mysqlv1.Rows
	json.Unmarshal(d.Payload, &rows)
	if len(rows.Rows) != 1 {
		t.Fatal(rows)
	}
	for _, sql := range []string{"SELECT id, name, price_cents FROM products WHERE id = 2001", "SELECT id, name, price_cents FROM products WHERE id > 1001", "SELECT id FROM different WHERE id = 1001"} {
		d, err = p.Execute(ctx, request("query", sql))
		if err == nil && d.Kind == "completed" {
			t.Fatal("wrong query accepted", sql)
		}
	}
}
func TestScenarioEntityFillIsSharedAndCannotChangeQA(t *testing.T) {
	p := runtimeFixture(t, true)
	ctx := context.Background()
	q := request("query", "SELECT id, name, price_cents FROM products WHERE id = 1001")
	d, err := p.Execute(ctx, q)
	if err != nil || d.Kind != "needs_data" {
		t.Fatal(d, err)
	}
	invalid := mysqlv1.EntityFill{SlotID: "p1", Rows: []map[string]mysqlv1.Value{{"id": mysqlv1.Int(1001), "name": mysqlv1.Text("键盘"), "price_cents": mysqlv1.Int(1)}}}
	resolution := pluginapi.Resolution{RequestID: q.ID, Continuation: d.Need.Continuation, StateVersion: d.Need.StateVersion, Payload: mysqlv1.Encode(invalid)}
	if _, err = p.Complete(ctx, resolution); err == nil {
		t.Fatal("QA violation accepted")
	}
	// Semantic rejection terminates the original request; a clean environment is required.
	p = runtimeFixture(t, true)
	d, err = p.Execute(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	invalid.Rows[0]["price_cents"] = mysqlv1.Int(9900)
	resolution.Continuation = d.Need.Continuation
	resolution.Payload = mysqlv1.Encode(invalid)
	raw, err := p.Complete(ctx, resolution)
	if err != nil {
		t.Fatal(err)
	}
	var rows mysqlv1.Rows
	json.Unmarshal(raw, &rows)
	if len(rows.Rows) != 1 {
		t.Fatal(rows)
	}
	q.ID = "next"
	d, err = p.Execute(ctx, q)
	if err != nil || d.Kind != "completed" {
		t.Fatal("filled entity not shared", d, err)
	}
}

func TestScenarioRejectsFillAfterConnectionCloses(t *testing.T) {
	p := runtimeFixture(t, true)
	ctx := context.Background()
	q := request("query", "SELECT id, name, price_cents FROM products WHERE id = 1001")
	d, err := p.Execute(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Execute(ctx, pluginapi.Request{ConnectionID: q.ConnectionID, Operation: "connection.close"}); err != nil {
		t.Fatal(err)
	}
	fill := mysqlv1.EntityFill{SlotID: "p1", Rows: []map[string]mysqlv1.Value{{"id": mysqlv1.Int(1001), "name": mysqlv1.Text("键盘"), "price_cents": mysqlv1.Int(9900)}}}
	if _, err = p.Complete(ctx, pluginapi.Resolution{RequestID: q.ID, Continuation: d.Need.Continuation, Payload: mysqlv1.Encode(fill)}); err == nil {
		t.Fatal("closed connection accepted late fill")
	}
}
