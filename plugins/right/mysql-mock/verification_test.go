package main

import (
	"context"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func TestPreparePreviewRejectsMissingApplicationWrite(t *testing.T) {
	spec, in, body := preparationFixture(t)
	sql := "INSERT INTO cart_items (user_id, product_id, quantity) VALUES (?, ?, ?)"
	source := sourceFile(t, spec.ProjectRoot, "Cart.java", "code", sql)
	in.Sources = append(in.Sources, source)
	body.Evidence = in.Sources
	params := []mysqlv1.Value{mysqlv1.Int(2001), mysqlv1.Int(1001), mysqlv1.Int(1)}
	statement := Statement{ID: "insert", SQL: sql, SourcePath: "Cart.java"}
	for i, name := range []string{"user_id", "product_id", "quantity"} {
		statement.Parameters = append(statement.Parameters, Parameter{Name: name, Type: "BIGINT", Allowed: []mysqlv1.Value{params[i]}})
	}
	body.DatabaseScenario.Statements = append(body.DatabaseScenario.Statements, statement)
	body.Preview = []PreviewStep{{StatementID: "insert", Params: params, Connection: "u1"}}
	body.Verification.FinalState = []StateAssertion{{Table: "cart_items", Where: Entity{"user_id": mysqlv1.Int(2001)}, Count: 1, Fields: Entity{"quantity": mysqlv1.Int(1)}}}
	spec.Input = mysqlv1.Encode(in)
	spec.Candidate = mysqlv1.Encode(body)
	report, err := (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatal(err, report)
	}
	body.Preview = []PreviewStep{{StatementID: "product", Params: []mysqlv1.Value{mysqlv1.Int(1001)}, Connection: "u1"}}
	spec.Candidate = mysqlv1.Encode(body)
	report, err = (&plugin{}).Prepare(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready {
		t.Fatal("preview passed despite missing application INSERT")
	}
}
