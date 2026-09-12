package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/app"
	"xmock.local/x-mock-mcp/internal/generation"
	jobrun "xmock.local/x-mock-mcp/internal/run"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/internal/trace"
	mcptransport "xmock.local/x-mock-mcp/internal/transport/mcp"
	"xmock.local/x-mock-mcp/pluginapi"
)

func copyExamples(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir("../examples", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "target" {
			return filepath.SkipDir
		}
		relative, err := filepath.Rel("..", path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		if d.IsDir() {
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
func callTool(t *testing.T, client *sdk.ClientSession, name string, input, out any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := client.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	raw, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError {
		t.Fatalf("%s: %s", name, raw)
	}
	if out != nil {
		if err = json.Unmarshal(raw, out); err != nil {
			t.Fatalf("%s decode: %v %s", name, err, raw)
		}
	}
}

func preserveDirectory(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(destination, 0700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, raw, 0600)
	})
	if err != nil {
		t.Fatal("preserve test evidence", err)
	}
}

// Retain the ordered business inputs/results while excluding transport IDs,
// timings and state-version offsets introduced by materializing the initial slot.
func businessTrace(t *testing.T, raw []byte) []byte {
	t.Helper()
	var result []any
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var event trace.Event
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Kind != "dependency.result" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		execution, ok := payload["execution"].(map[string]any)
		if !ok || execution["source"] != "stateful" {
			continue
		}
		delete(execution, "state_version")
		var request map[string]any
		if err := json.Unmarshal(event.Request.Payload, &request); err != nil {
			t.Fatal(err)
		}
		delete(request, "statement_id")
		result = append(result, map[string]any{"request": request, "result": payload})
	}
	if len(result) == 0 {
		t.Fatal("no business execution evidence")
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
func TestAgentFlowAndReplay(t *testing.T) {
	root := copyExamples(t)
	repo, _ := filepath.Abs("..")
	config := app.Config{Jobs: []jobrun.JobSpec{{Name: "shopping-browser", Argv: []string{"mvn", "-B", "-ntp", "-o", "-f", "examples/springboot-shop/pom.xml", "-Dtest=ShoppingBrowserTest", "test"}, WorkingDirectory: ".", TimeoutMS: 240000, Env: map[string]string{"JAVA_HOME": javaHome(t), "PLAYWRIGHT_BROWSERS_PATH": browserHome(), "X_MOCK_MYSQL_URL": "jdbc:mysql://${endpoint.app.mysql}/app?sslMode=DISABLED&useServerPrepStmts=true&emulateUnsupportedPstmts=false&cachePrepStmts=false&socketTimeout=180000&connectionCollation=utf8mb4_bin"}}}}
	service, err := app.Open(root, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close(context.Background()) })
	server := httptest.NewServer(mcptransport.HTTP(mcptransport.New(service), "token", ""))
	defer server.Close()
	client, err := mcptransport.Connect(context.Background(), server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	observer, err := mcptransport.Connect(context.Background(), server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	refs := map[pluginapi.Role]pluginapi.Reference{}
	for _, source := range []string{"./plugins/left/mysql-wire", "./plugins/right/mysql-mock"} {
		archive, sha, ref := packagePlugin(t, source, "")
		callTool(t, client, "mock_plugin_install", map[string]any{"package_path": archive, "sha256": sha}, nil)
		callTool(t, client, "mock_plugin_enable", map[string]any{"role": ref.Role, "plugin_id": ref.ID, "version": ref.Version}, nil)
		refs[ref.Role] = ref
	}
	list, err := client.ListTools(context.Background(), nil)
	if err != nil || len(list.Tools) != 16 {
		t.Fatal("plugin changed MCP surface", list, err)
	}
	args := map[string]any{"role": "right", "plugin_id": "mysql-mock", "version": "0.1.0", "schema_name": "qa_input"}
	var capability map[string]any
	callTool(t, client, "mock_capabilities", args, &capability)
	if capability["schema"] == nil {
		t.Fatal("missing QA schema")
	}
	delete(args, "schema_name")
	input, _ := os.ReadFile(filepath.Join(root, "examples/scenarios/shop-input.json"))
	args["input"] = json.RawMessage(input)
	var report pluginapi.PreparationReport
	callTool(t, client, "mock_scenario_prepare", args, &report)
	if report.Ready || len(service.Bindings.List()) != 0 {
		t.Fatal("prepare opened listener or claimed candidate ready")
	}
	candidate, _ := os.ReadFile(filepath.Join(root, "examples/scenarios/shop-explore-candidate.json"))
	args["candidate"] = json.RawMessage(candidate)
	callTool(t, client, "mock_scenario_prepare", args, &report)
	if !report.Ready {
		t.Fatalf("prepare: %+v", report)
	}
	document := scenario.Document{ID: "shop", PluginID: "mysql-mock", PluginVersion: "0.1.0", ContractID: "mysql.operation", ContractVersion: 1, Input: input, Body: report.CompiledBody}
	callTool(t, client, "mock_scenario_put", map[string]any{"document": document, "expected_version": 0}, &document)
	var generatedID string
	var baseline []byte
	for iteration := 0; iteration < 4; iteration++ {
		mode := "StrictReplay"
		if iteration == 0 {
			mode = "AgentFill"
		}
		request := app.EnvironmentRequest{ScenarioID: document.ID, ScenarioVersion: document.Version, DataStrategy: mode, TimeoutMS: 180000, Bindings: []app.BindingRequest{{ResourceID: "app", Left: refs[pluginapi.Left], Right: refs[pluginapi.Right], Contract: pluginapi.Contract{ID: "mysql.operation", Version: 1}, LeftConfig: json.RawMessage(`{"host":"127.0.0.1","port":0,"username":"mock","password":"mock-local"}`), RightConfig: json.RawMessage(fmt.Sprintf(`{"mode":%q}`, mode))}}}
		var env app.Environment
		callTool(t, client, "mock_environment_create", request, &env)
		var run jobrun.Status
		started := time.Now()
		callTool(t, client, "mock_run_start", map[string]string{"environment_id": env.ID, "job_name": "shopping-browser"}, &run)
		if time.Since(started) > 3*time.Second {
			t.Fatal("run_start blocked on test")
		}
		if iteration == 0 {
			var batch generation.Batch
			owner := ""
			deadline := time.Now().Add(50 * time.Second)
			for len(batch.Requests) == 0 && time.Now().Before(deadline) {
				callTool(t, client, "mock_requests_next", generation.NextRequest{RunID: run.ID, OwnerToken: owner, Limit: 1, WaitMS: 2000}, &batch)
				owner = batch.OwnerToken
			}
			if len(batch.Requests) != 1 {
				status, _ := service.Runs.Status(run.ID)
				log, _ := os.ReadFile(status.LogPath)
				t.Fatalf("missing generation: %+v\n%s", status, log)
			}
			leased := batch.Requests[0]
			generatedID = leased.Need.RequestID
			var contextData struct {
				Slot struct {
					ID    string          `json:"id"`
					Fixed json.RawMessage `json:"fixed"`
				} `json:"slot"`
			}
			if err = json.Unmarshal(leased.Need.Context, &contextData); err != nil {
				t.Fatal(err)
			}
			fill := json.RawMessage(fmt.Sprintf(`{"slot_id":%q,"rows":[%s]}`, contextData.Slot.ID, contextData.Slot.Fixed))
			callTool(t, observer, "mock_run_status", map[string]string{"run_id": run.ID}, nil)
			before, _ := json.Marshal(service.Bindings.List())
			var takeover generation.Batch
			callTool(t, observer, "mock_requests_next", generation.NextRequest{RunID: run.ID, Limit: 1, Takeover: true, ExpectedOwnerEpoch: batch.OwnerEpoch}, &takeover)
			if takeover.OwnerEpoch != batch.OwnerEpoch+1 || len(takeover.Requests) != 1 || takeover.Requests[0].Need.RequestID != generatedID {
				t.Fatal("takeover lost pending request", takeover)
			}
			denied, err := client.CallTool(context.Background(), &sdk.CallToolParams{Name: "mock_requests_resolve", Arguments: generation.ResolveRequest{RunID: run.ID, RequestID: generatedID, OwnerToken: owner, LeaseToken: leased.LeaseToken, Candidate: fill}})
			if err != nil || !denied.IsError {
				t.Fatal("stale owner accepted", denied, err)
			}
			failure, _ := json.Marshal(denied.StructuredContent)
			if !bytes.Contains(failure, []byte("OWNER_CONFLICT")) {
				t.Fatal("wrong stale-owner failure", string(failure))
			}
			after, _ := json.Marshal(service.Bindings.List())
			if !bytes.Equal(before, after) {
				t.Fatal("second client changed listeners or binding instances")
			}
			var ack generation.Ack
			callTool(t, observer, "mock_requests_resolve", generation.ResolveRequest{RunID: run.ID, RequestID: generatedID, OwnerToken: takeover.OwnerToken, LeaseToken: takeover.Requests[0].LeaseToken, Candidate: fill}, &ack)
			if ack.State != "resolved" {
				t.Fatal("premature completion", ack)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		err = service.Runs.Wait(ctx, run.ID)
		cancel()
		status, _ := service.Runs.Status(run.ID)
		log, _ := os.ReadFile(status.LogPath)
		if err != nil || status.State != "succeeded" {
			t.Fatalf("%s: %+v %v\n%s", mode, status, err, log)
		}
		t.Logf("%s run=%s succeeded", mode, run.ID)
		artifacts := filepath.Join(repo, "artifacts/p0", run.ID)
		os.MkdirAll(artifacts, 0700)
		os.WriteFile(filepath.Join(artifacts, "maven.log"), log, 0600)
		events, _ := os.ReadFile(filepath.Join(root, ".x-mock/traces", run.ID+".ndjson"))
		os.WriteFile(filepath.Join(artifacts, "trace.ndjson"), events, 0600)
		preserveDirectory(t, filepath.Join(root, "examples/springboot-shop/target/browser", run.ID), filepath.Join(artifacts, "browser"))
		preserveDirectory(t, filepath.Join(root, "examples/springboot-shop/target/surefire-reports"), filepath.Join(artifacts, "junit"))
		business := businessTrace(t, events)
		os.WriteFile(filepath.Join(artifacts, "business-trace.json"), business, 0600)
		if iteration == 0 {
			baseline = business
		} else if !bytes.Equal(baseline, business) {
			t.Fatal("fresh replay changed ordered business results; see business-trace.json")
		}
		if iteration == 0 {
			var exported scenario.Document
			callTool(t, client, "mock_scenario_export", map[string]any{"run_id": run.ID, "request_ids": []string{generatedID}}, &exported)
			if !strings.Contains(string(exported.Body), `"cart_items":[]`) {
				t.Fatal("export preloaded terminal cart")
			}
			callTool(t, client, "mock_scenario_put", map[string]any{"document": exported, "expected_version": document.Version}, &document)
		} else {
			if strings.Contains(string(events), `"generation.confirmed"`) {
				t.Fatal("replay generated data")
			}
		}
		callTool(t, client, "mock_environment_destroy", map[string]string{"environment_id": env.ID}, nil)
	}
	for _, role := range []pluginapi.Role{pluginapi.Left, pluginapi.Right} {
		ref := refs[role]
		args := map[string]any{"role": role, "plugin_id": ref.ID, "version": ref.Version}
		callTool(t, client, "mock_plugin_disable", args, nil)
		callTool(t, client, "mock_plugin_uninstall", args, nil)
		if role == pluginapi.Left && len(service.Catalog.List()) != 1 {
			t.Fatal("left uninstall removed right")
		}
	}
}
