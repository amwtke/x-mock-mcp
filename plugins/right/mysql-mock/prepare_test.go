package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

const testDDL = `CREATE TABLE products (id BIGINT NOT NULL PRIMARY KEY, name VARCHAR(128) NOT NULL, price_cents BIGINT NOT NULL); CREATE TABLE cart_items (id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, user_id BIGINT NOT NULL, product_id BIGINT NOT NULL, quantity BIGINT NOT NULL, UNIQUE KEY uq_cart (user_id, product_id), FOREIGN KEY (product_id) REFERENCES products(id));`

func sourceFile(t *testing.T, root, path, kind, content string) Source {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	return Source{Path: path, Kind: kind, SHA256: hex.EncodeToString(sum[:])}
}
func preparationFixture(t *testing.T) (pluginapi.PreparationSpec, Input, Bundle) {
	t.Helper()
	root := t.TempDir()
	sql := "SELECT id, name, price_cents FROM products WHERE id = ?"
	qa := QA{ID: "shop", Goal: "浏览商品并加入购物车", NaturalLanguage: "打开首页，商品价格 99 元；加入后可在购物车查看。", StartPage: "/", Role: "U1", InitialState: "有商品，购物车为空", DataPolicy: "价格固定，ID 可以生成", WriteRules: "重复加入累计数量，不扣库存", Coverage: "正常流程，其他异常另立用例", Steps: []QAStep{{ID: "S1", Action: "点击商品", Expected: "名称键盘，价格99元"}}, Fixed: map[string]string{"price_cents": "9900"}}
	sources := []Source{sourceFile(t, root, "schema.sql", "ddl", testDDL), sourceFile(t, root, "Repository.java", "code", sql)}
	in := Input{QA: qa, Sources: sources}
	body := Bundle{QAContract: qa, Evidence: sources, DatabaseScenario: DatabaseScenario{Database: "app", Mode: "stateful", Initial: map[string][]Entity{"products": {{"id": mysqlv1.Int(1001), "name": mysqlv1.Text("键盘"), "price_cents": mysqlv1.Int(9900)}}, "cart_items": {}}, Statements: []Statement{{ID: "product", SQL: sql, SourcePath: "Repository.java", Parameters: []Parameter{{Name: "id", Type: "BIGINT", Allowed: []mysqlv1.Value{mysqlv1.Int(1001)}}}}}}, StepBindings: []StepBinding{{StepID: "S1", API: "GET /api/products/1001", SourcePath: "Repository.java", Statements: []string{"product"}}}, APIExpectations: []APIExpectation{{StepID: "S1", Method: "GET", Path: "/api/products/1001", Status: 200}}, DataBindings: []DataBinding{{QAKey: "price_cents", Table: "products", Key: "1001", Column: "price_cents"}}}
	return pluginapi.PreparationSpec{ProjectRoot: root, Input: mysqlv1.Encode(in), Candidate: mysqlv1.Encode(body)}, in, body
}
func TestPrepareAcceptsGroundedCandidate(t *testing.T) {
	spec, _, _ := preparationFixture(t)
	p := &plugin{}
	report, err := p.Prepare(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready {
		t.Fatalf("not ready: %+v", report)
	}
	if err = report.Validate(); err != nil {
		t.Fatal(err)
	}
	var out Bundle
	if err = json.Unmarshal(report.CompiledBody, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.DatabaseScenario.Tables) != 2 {
		t.Fatal("DDL was not compiled")
	}
}
func TestPrepareRejectsMissingAmbiguousAndChangedEvidence(t *testing.T) {
	for _, tc := range []string{"missing-expectation", "ambiguity", "stale-evidence", "changed-price", "unsupported-decimal", "missing-ddl"} {
		t.Run(tc, func(t *testing.T) {
			spec, in, body := preparationFixture(t)
			switch tc {
			case "missing-expectation":
				in.QA.Steps[0].Expected = ""
			case "ambiguity":
				in.QA.WriteRules = ""
				in.QA.Ambiguities = []string{"加入是否扣库存"}
			case "stale-evidence":
				os.WriteFile(filepath.Join(spec.ProjectRoot, "Repository.java"), []byte("changed"), 0600)
			case "changed-price":
				body.DatabaseScenario.Initial["products"][0]["price_cents"] = mysqlv1.Int(1)
			case "unsupported-decimal":
				in.Sources[0] = sourceFile(t, spec.ProjectRoot, "schema.sql", "ddl", "CREATE TABLE products (id BIGINT PRIMARY KEY, price DECIMAL(10,2));")
			case "missing-ddl":
				in.Sources = in.Sources[1:]
			}
			spec.Input = mysqlv1.Encode(in)
			spec.Candidate = mysqlv1.Encode(body)
			report, err := (&plugin{}).Prepare(context.Background(), spec)
			if err != nil {
				t.Fatal(err)
			}
			if report.Ready {
				t.Fatal("invalid input accepted")
			}
		})
	}
}
func TestCompileRetainsSQLPredicates(t *testing.T) {
	tables, err := ParseDDL(testDDL)
	if err != nil {
		t.Fatal(err)
	}
	statement := Statement{ID: "cart", SQL: "SELECT id, quantity FROM cart_items WHERE user_id = ? AND product_id = ?", Parameters: []Parameter{{Name: "user_id", Type: "BIGINT"}, {Name: "product_id", Type: "BIGINT"}}}
	plan, err := Compile(statement, tables, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Predicates) != 2 || plan.Parameters[0].Name != "user_id" {
		t.Fatalf("predicate lost: %+v", plan)
	}
	statement.SQL = "SELECT id FROM cart_items WHERE user_id > ? AND product_id = ?"
	if _, err = Compile(statement, tables, "app"); err == nil {
		t.Fatal("unsupported operator became equality")
	}
	statement.SQL = "UPDATE cart_items SET quantity = 2"
	if _, err = Compile(statement, tables, "app"); err == nil {
		t.Fatal("unbounded update accepted")
	}
}

func TestPreparePreservesQAInitialAndFinalStateAssertions(t *testing.T) {
	spec, in, body := preparationFixture(t)
	in.QA.InitialAssertions = []StateAssertion{{Table: "cart_items", Where: Entity{}, Count: 0}}
	in.QA.FinalAssertions = []StateAssertion{{Table: "cart_items", Where: Entity{}, Count: 0}}
	body.QAContract = in.QA
	body.Verification.FinalState = in.QA.FinalAssertions
	body.DatabaseScenario.Initial["cart_items"] = []Entity{{"id": mysqlv1.Int(5001), "user_id": mysqlv1.Int(2001), "product_id": mysqlv1.Int(1001), "quantity": mysqlv1.Int(1)}}
	spec.Input = mysqlv1.Encode(in)
	spec.Candidate = mysqlv1.Encode(body)
	report, err := (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || report.Ready {
		t.Fatal("preloaded final cart accepted", report, err)
	}
	body.DatabaseScenario.Initial["cart_items"] = []Entity{}
	body.Verification.FinalState = nil
	spec.Candidate = mysqlv1.Encode(body)
	report, err = (&plugin{}).Prepare(context.Background(), spec)
	if err != nil || report.Ready {
		t.Fatal("QA final assertion erased", report, err)
	}
}

func TestPrepareCanonicalBundleCanBeSavedWithEmptyOptionalQAFields(t *testing.T) {
	spec, in, body := preparationFixture(t)
	in.QA.Ambiguities = []string{}
	body.QAContract = in.QA
	spec.Input = mysqlv1.Encode(in)
	spec.Candidate = mysqlv1.Encode(body)
	// A client may explicitly send an empty optional array even though the
	// canonical producer omits it. It has the same meaning when re-prepared.
	var input map[string]any
	json.Unmarshal(spec.Input, &input)
	input["qa"].(map[string]any)["ambiguities"] = []any{}
	spec.Input = mysqlv1.Encode(input)
	p := &plugin{}
	report, err := p.Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatal(report, err)
	}
	spec.Candidate = report.CompiledBody
	report, err = p.Prepare(context.Background(), spec)
	if err != nil || !report.Ready {
		t.Fatal("canonical bundle cannot be saved", report, err)
	}
}
