package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/app"
	"xmock.local/x-mock-mcp/internal/binding"
	jobrun "xmock.local/x-mock-mcp/internal/run"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestShoppingDefectsKeepQAExpectations(t *testing.T) {
	shoppingDefects(t, "shop", "ShopFlowTest", "jdbc")
}

func shoppingDefects(t *testing.T, fixture, testClass, orm string) {
	t.Helper()
	type packaged struct {
		path, sha string
		ref       pluginapi.Reference
	}
	packages := []packaged{}
	for _, source := range []string{"./plugins/left/mysql-wire", "./plugins/right/mysql-mock"} {
		p, d, r := packagePlugin(t, source, "")
		packages = append(packages, packaged{p, d, r})
	}
	for _, defect := range shoppingDefectCases(orm, fixture) {
		t.Run(defect.name, func(t *testing.T) {
			root := copyExamples(t)
			source := defect.source
			path := filepath.Join(root, source)
			raw, _ := os.ReadFile(path)
			code := strings.Replace(string(raw), defect.before, defect.after, 1)
			if code == string(raw) {
				t.Fatal("mutation did not change code")
			}
			os.WriteFile(path, []byte(code), 0600)
			var input, body map[string]any
			inputRaw, _ := os.ReadFile(filepath.Join(root, "examples/scenarios/"+fixture+"-input.json"))
			candidateRaw, _ := os.ReadFile(filepath.Join(root, "examples/scenarios/"+fixture+"-candidate.json"))
			json.Unmarshal(inputRaw, &input)
			json.Unmarshal(candidateRaw, &body)
			// Refresh actual evidence, preserving the complete QA and expected state.
			sum := sha256.Sum256([]byte(code))
			for _, value := range input["sources"].([]any) {
				entry := value.(map[string]any)
				if entry["path"] == source {
					entry["sha256"] = hex.EncodeToString(sum[:])
				}
			}
			body["evidence"] = input["sources"]
			inputRaw, _ = json.Marshal(input)
			candidateRaw, _ = json.Marshal(body)
			config := app.Config{Jobs: []jobrun.JobSpec{{Name: "negative", Argv: []string{"mvn", "-B", "-ntp", "-o", "-f", "examples/springboot-shop/pom.xml", "-Dtest=" + testClass, "test"}, WorkingDirectory: ".", TimeoutMS: 30000, Env: map[string]string{"JAVA_HOME": javaHome(t), "X_MOCK_MYSQL_URL": "jdbc:mysql://${endpoint.app.mysql}/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=10000&connectionCollation=utf8mb4_bin"}}}}
			service, err := app.Open(root, config)
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close(context.Background())
			for _, p := range packages {
				if _, err = service.Catalog.Install(context.Background(), p.path, p.sha); err != nil {
					t.Fatal(err)
				}
				service.Catalog.Enable(p.ref)
			}
			report, err := service.Prepare(context.Background(), packages[1].ref, inputRaw, candidateRaw)
			if err != nil {
				t.Fatal(err)
			}
			if defect.prepareReject {
				if report.Ready {
					t.Fatal("undeclared SQL/entity change accepted")
				}
				for _, issue := range report.Diagnostics {
					if strings.Contains(issue.Message, "STALE_EVIDENCE") {
						t.Fatal("only stale evidence caught mutation")
					}
				}
				t.Logf("%s rejected by source validation with refreshed SHA: %+v", defect.name, report.Diagnostics)
				return
			}
			if !report.Ready {
				t.Fatalf("grounded candidate unexpectedly failed prepare: %+v", report)
			}
			doc, err := service.Scenarios.Put(context.Background(), scenario.Document{ID: "negative", PluginID: "mysql-mock", PluginVersion: packages[1].ref.Version, ContractID: "mysql.operation", ContractVersion: 1, Input: inputRaw, Body: report.CompiledBody}, 0)
			if err != nil {
				t.Fatal(err)
			}
			env, err := service.CreateEnvironment(context.Background(), app.EnvironmentRequest{ScenarioID: doc.ID, ScenarioVersion: doc.Version, DataStrategy: "StrictReplay", TimeoutMS: 10000, Bindings: []app.BindingRequest{{ResourceID: "app", Left: packages[0].ref, Right: packages[1].ref, Contract: pluginapi.Contract{ID: "mysql.operation", Version: 1}, LeftConfig: json.RawMessage(`{"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}`), RightConfig: json.RawMessage(`{"mode":"StrictReplay"}`)}}})
			if err != nil {
				t.Fatal(err)
			}
			run, err := service.StartRun(env.ID, "negative")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			err = service.Runs.Wait(ctx, run.ID)
			cancel()
			status, _ := service.Runs.Status(run.ID)
			log, _ := os.ReadFile(status.LogPath)
			if err != nil || status.State != "failed" {
				t.Fatalf("defect passed %s %+v %v\n%s", defect.name, status, err, log)
			}
			if !strings.Contains(string(log), "AssertionFailedError") {
				t.Fatalf("did not reach business assertions\n%s", log)
			}
			stage := map[string]string{"jdbc": "p0", "mybatis": "p1", "plus": "p1-plus"}[orm]
			if err := os.MkdirAll(filepath.Join("../artifacts", stage), 0700); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join("../artifacts", stage, defect.name+"-"+binding.ID()+".log")
			os.WriteFile(archive, log, 0600)
			t.Logf("%s rejected by real HTTP/QA assertions; run=%s", defect.name, run.ID)
		})
	}
}
