package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/app"
	jobrun "xmock.local/x-mock-mcp/internal/run"
	"xmock.local/x-mock-mcp/internal/scenario"
	mcptransport "xmock.local/x-mock-mcp/internal/transport/mcp"
	"xmock.local/x-mock-mcp/pluginapi"
)

func copyOrderExample(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir("../example/springboot", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "target" || entry.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel("..", path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSpringBootOrderExample(t *testing.T) {
	type packaged struct {
		path, digest string
		ref          pluginapi.Reference
	}
	packages := []packaged{}
	for _, path := range []string{"./plugins/left/mysql-wire", "./plugins/right/mysql-mock"} {
		archive, digest, ref := packagePlugin(t, path, "")
		packages = append(packages, packaged{archive, digest, ref})
	}
	type testCase struct {
		name, klass, before, after string
		orders                     int
	}
	for _, tc := range []testCase{
		{name: "http", klass: "OrderFlowTest"},
		{name: "browser", klass: "OrderBrowserTest"},
		{name: "missing-insert", klass: "OrderFlowTest", before: "long id = repository.insert(user, product, quantity, total);", after: "long id = 9001;", orders: 0},
		{name: "missing-stock-update", klass: "OrderFlowTest", before: "if (!repository.reserveStock(product.id(), product.stock(), product.stock() - quantity))", after: "if (false)", orders: 1},
		{name: "forced-rollback", klass: "OrderFlowTest", before: "return repository.order(id, user).orElseThrow(() -> new IllegalStateException", after: "org.springframework.transaction.interceptor.TransactionAspectSupport.currentTransactionStatus().setRollbackOnly(); return repository.order(id, user).orElseThrow(() -> new IllegalStateException", orders: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := copyOrderExample(t)
			var input, body map[string]any
			inputRaw, err := os.ReadFile(filepath.Join(root, "example/springboot/mock/input.json"))
			if err != nil {
				t.Fatal(err)
			}
			candidateRaw, err := os.ReadFile(filepath.Join(root, "example/springboot/mock/candidate.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(inputRaw, &input); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(candidateRaw, &body); err != nil {
				t.Fatal(err)
			}
			if tc.before != "" {
				source := "example/springboot/src/main/java/local/xmock/order/OrderService.java"
				path := filepath.Join(root, source)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				code := strings.Replace(string(raw), tc.before, tc.after, 1)
				if code == string(raw) {
					t.Fatal("mutation did not change source")
				}
				if err = os.WriteFile(path, []byte(code), 0600); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256([]byte(code))
				for _, sourceEvidence := range input["sources"].([]any) {
					entry := sourceEvidence.(map[string]any)
					if entry["path"] == source {
						entry["sha256"] = hex.EncodeToString(sum[:])
					}
				}
				body["evidence"] = input["sources"]
				inputRaw, _ = json.Marshal(input)
				candidateRaw, _ = json.Marshal(body)
			}
			argv := []string{"mvn", "-B", "-ntp", "-o", "-f", "example/springboot/pom.xml", "-Dtest=" + tc.klass, "test"}
			if tc.before != "" {
				argv = append(argv, fmt.Sprintf("-Dxmock.failure.expectedOrders=%d", tc.orders))
			}
			config := app.Config{Jobs: []jobrun.JobSpec{{Name: "orders", Argv: argv, WorkingDirectory: ".", TimeoutMS: 120000, Env: map[string]string{
				"JAVA_HOME": javaHome(t), "PLAYWRIGHT_BROWSERS_PATH": browserHome(),
				"X_MOCK_MYSQL_URL": "jdbc:mysql://${endpoint.app.mysql}/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin",
			}}}}
			service, err := app.Open(root, config)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close(context.Background())
			server := httptest.NewServer(mcptransport.HTTP(mcptransport.New(service), "test-token", ""))
			defer server.Close()
			connection, err := mcptransport.Connect(context.Background(), server.URL, "test-token")
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			for _, p := range packages {
				callTool(t, connection, "mock_plugin_install", map[string]any{"package_path": p.path, "sha256": p.digest}, nil)
				callTool(t, connection, "mock_plugin_enable", map[string]any{"role": p.ref.Role, "plugin_id": p.ref.ID, "version": p.ref.Version}, nil)
			}
			var report pluginapi.PreparationReport
			callTool(t, connection, "mock_scenario_prepare", map[string]any{"role": "right", "plugin_id": "mysql-mock", "version": packages[1].ref.Version, "input": json.RawMessage(inputRaw), "candidate": json.RawMessage(candidateRaw)}, &report)
			if !report.Ready {
				t.Fatalf("source-grounded candidate: %+v", report)
			}
			doc := scenario.Document{ID: "orders", PluginID: "mysql-mock", PluginVersion: packages[1].ref.Version, ContractID: "mysql.operation", ContractVersion: 1, Input: inputRaw, Body: report.CompiledBody}
			callTool(t, connection, "mock_scenario_put", map[string]any{"document": doc, "expected_version": 0}, &doc)
			var environment app.Environment
			callTool(t, connection, "mock_environment_create", app.EnvironmentRequest{ScenarioID: doc.ID, ScenarioVersion: doc.Version, DataStrategy: "StrictReplay", TimeoutMS: 10000, Bindings: []app.BindingRequest{{ResourceID: "app", Left: packages[0].ref, Right: packages[1].ref, Contract: pluginapi.Contract{ID: "mysql.operation", Version: 1}, LeftConfig: json.RawMessage(`{"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}`), RightConfig: json.RawMessage(`{"mode":"StrictReplay"}`)}}}, &environment)
			var run jobrun.Status
			callTool(t, connection, "mock_run_start", map[string]string{"environment_id": environment.ID, "job_name": "orders"}, &run)
			ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
			err = service.Runs.Wait(ctx, run.ID)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			status, err := service.Runs.Status(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			log, err := os.ReadFile(status.LogPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.before == "" {
				if status.State != "succeeded" {
					t.Fatalf("%+v\n%s", status, log)
				}
			} else {
				if status.State != "failed" || !strings.Contains(string(log), "AssertionFailedError") {
					t.Fatalf("defect escaped business assertions: %+v\n%s", status, log)
				}
				if !strings.Contains(string(log), fmt.Sprintf("Defect-state audit passed: stock=10 orders=%d", tc.orders)) {
					t.Fatalf("missing state audit before failed run cleanup\n%s", log)
				}
			}
			archive := filepath.Join("../artifacts/orders", tc.name+"-"+run.ID)
			if err = os.MkdirAll(archive, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(archive, "maven.log"), log, 0600); err != nil {
				t.Fatal(err)
			}
			events, err := os.ReadFile(filepath.Join(root, ".x-mock/traces", run.ID+".ndjson"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(archive, "trace.ndjson"), events, 0600); err != nil {
				t.Fatal(err)
			}
			preserveDirectory(t, filepath.Join(root, "example/springboot/target/surefire-reports"), filepath.Join(archive, "junit"))
			if tc.name == "browser" {
				preserveDirectory(t, filepath.Join(root, "example/springboot/target/browser", run.ID), filepath.Join(archive, "browser"))
			}
			callTool(t, connection, "mock_environment_destroy", map[string]string{"environment_id": environment.ID}, nil)
			for _, p := range packages {
				args := map[string]any{"role": p.ref.Role, "plugin_id": p.ref.ID, "version": p.ref.Version}
				callTool(t, connection, "mock_plugin_disable", args, nil)
				callTool(t, connection, "mock_plugin_uninstall", args, nil)
			}
			t.Logf("%s: expected run state=%s, evidence=%s", tc.name, status.State, archive)
		})
	}
}
