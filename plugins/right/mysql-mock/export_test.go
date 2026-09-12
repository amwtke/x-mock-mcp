package main

import (
	"context"
	"encoding/json"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestExportKeepsInitialCartAndMaterializesSelectedReference(t *testing.T) {
	p := runtimeFixture(t, true)
	ctx := context.Background()
	q := request("query", "SELECT id, name, price_cents FROM products WHERE id = 1001")
	need, err := p.Execute(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	fill := mysqlv1.EntityFill{SlotID: "p1", Rows: []map[string]mysqlv1.Value{{"id": mysqlv1.Int(1001), "name": mysqlv1.Text("键盘"), "price_cents": mysqlv1.Int(9900)}}}
	candidate := mysqlv1.Encode(fill)
	result, err := p.Complete(ctx, pluginapi.Resolution{RequestID: q.ID, Continuation: need.Need.Continuation, Payload: candidate})
	if err != nil {
		t.Fatal(err)
	}
	insert := plan(t, p.store.tables, "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)", 3)
	if _, err = p.store.execute(ctx, "app", insert, []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(2)}); err != nil {
		t.Fatal(err)
	}
	exported, err := p.Export(ctx, pluginapi.ExportSpec{Snapshot: mysqlv1.Encode(p.bundle), Captures: []pluginapi.Capture{{Request: q, Candidate: candidate, Response: result}}})
	if err != nil {
		t.Fatal(err)
	}
	tampered := append(json.RawMessage(" "), candidate...)
	if _, err := p.Export(ctx, pluginapi.ExportSpec{Snapshot: mysqlv1.Encode(p.bundle), Captures: []pluginapi.Capture{{Request: q, Candidate: tampered, Response: result}}}); err == nil {
		t.Fatal("accepted a candidate that was never confirmed")
	}
	var body Bundle
	if err = json.Unmarshal(exported, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.DatabaseScenario.Initial["cart_items"]) != 0 {
		t.Fatal("export copied terminal shopping cart")
	}
	if len(body.DatabaseScenario.Initial["products"]) != 1 || body.Replay.RequiresGeneration {
		t.Fatal("reference entity not materialized")
	}
}
