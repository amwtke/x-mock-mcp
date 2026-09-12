package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	jobrun "xmock.local/x-mock-mcp/internal/run"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/internal/trace"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestServiceRejectsUnknownStrategyAndUnpreparedScenario(t *testing.T) {
	s, err := Open(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if _, err = s.CreateEnvironment(context.Background(), EnvironmentRequest{DataStrategy: "ProviderFill"}); pluginapi.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
	_, err = s.Scenarios.Put(context.Background(), scenario.Document{ID: "shop", PluginID: "mysql-mock", PluginVersion: "0.1.0", ContractID: "mysql.operation", ContractVersion: 1, Input: json.RawMessage(`{}`), Body: json.RawMessage(`{}`)}, 0)
	if pluginapi.Code(err) != "PLUGIN_NOT_INSTALLED" {
		t.Fatal("bypassed preparation", err)
	}
}

func TestTraceFailureMarksRunFailed(t *testing.T) {
	s, err := Open(t.TempDir(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	s.Trace, err = trace.Open(s.Root, 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Runs.Start("tracefull", "env", jobrun.JobSpec{Name: "wait", Argv: []string{"/bin/sh", "-c", "sleep 30"}, WorkingDirectory: ".", TimeoutMS: 5000}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &environment{requests: map[string]pluginapi.Request{}}
	s.observe(e, pluginapi.Request{ID: "q", RunID: r.ID}, json.RawMessage(`{}`), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = s.Runs.Wait(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	status, _ := s.Runs.Status(r.ID)
	if status.State != "failed" || status.Error == nil || status.Error.Code != "TRACE_FULL" {
		t.Fatal(status)
	}
}
func TestDaemonOwnershipAndIndependentProjects(t *testing.T) {
	a := t.TempDir()
	lease, err := AcquireDaemon(a)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if second, err := AcquireDaemon(a); err == nil {
		second.Close()
		t.Fatal("two daemons acquired same project")
	}
	other, err := AcquireDaemon(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	if _, err = os.Stat(filepath.Join(a, ".x-mock", "daemon.lock")); err != nil {
		t.Fatal(err)
	}
}
