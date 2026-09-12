package integration

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	mcptransport "xmock.local/x-mock-mcp/internal/transport/mcp"
)

// Exercise real stdio subprocesses: a client reconnect must not start another
// daemon or create an independent plugin registry.
func TestMCPCompatibility(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "x-mock")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", bin, "./cmd/x-mock")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	root := t.TempDir()
	log, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	daemon := exec.Command(bin, "serve", "--root", root, "--listen", "127.0.0.1:0")
	daemon.Stdout, daemon.Stderr = log, log
	if err = daemon.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- daemon.Wait() }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		daemon.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			daemon.Process.Kill()
			<-done
			t.Error("daemon did not shut down")
		}
	}
	t.Cleanup(stop)
	readyPath := filepath.Join(root, ".x-mock/ready.json")
	var ready struct {
		Address    string `json:"address"`
		PID        int    `json:"pid"`
		InstanceID string `json:"instance_id"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(readyPath)
		if json.Unmarshal(raw, &ready) == nil && ready.Address != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ready.PID != daemon.Process.Pid || ready.InstanceID == "" {
		t.Fatal("daemon did not publish readiness", ready)
	}
	token, err := os.ReadFile(filepath.Join(root, ".x-mock/token"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(root, ".x-mock/token"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("token permissions", info.Mode())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	httpClient, err := mcptransport.Connect(ctx, ready.Address, strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.Close()
	bridge := func(name string) *sdk.ClientSession {
		t.Helper()
		command := exec.Command(bin, "mcp", "stdio", "--root", root)
		command.Stderr = log
		client := sdk.NewClient(&sdk.Implementation{Name: name, Version: "test"}, nil)
		session, err := client.Connect(ctx, &sdk.CommandTransport{Command: command, TerminateDuration: time.Second}, nil)
		if err != nil {
			t.Fatal("stdio initialize", err)
		}
		t.Cleanup(func() { session.Close() })
		return session
	}
	first, second := bridge("codex-contract"), bridge("claude-contract")
	names := func(client *sdk.ClientSession) string {
		listed, err := client.ListTools(ctx, nil)
		if err != nil || len(listed.Tools) != 16 {
			t.Fatal("tools/list", listed, err)
		}
		var names []string
		for _, tool := range listed.Tools {
			names = append(names, tool.Name)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	if names(httpClient) != names(first) || names(first) != names(second) {
		t.Fatal("transport tool contracts differ")
	}
	archive, sha, ref := packagePlugin(t, "./internal/testkit/plugins/left-echo", "")
	callTool(t, httpClient, "mock_plugin_install", map[string]any{"package_path": archive, "sha256": sha}, nil)
	args := map[string]any{"role": ref.Role, "plugin_id": ref.ID, "version": ref.Version}
	callTool(t, first, "mock_plugin_enable", args, nil)
	var caps struct {
		Plugins []catalog.Record `json:"plugins"`
	}
	callTool(t, second, "mock_capabilities", map[string]any{}, &caps)
	if len(caps.Plugins) != 1 || !caps.Plugins[0].Enabled {
		t.Fatal("stdio clients do not share HTTP registry", caps)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	reconnected := bridge("codex-reconnect")
	if names(reconnected) != names(httpClient) {
		t.Fatal("reconnect changed tools")
	}
	callTool(t, reconnected, "mock_capabilities", map[string]any{}, &caps)
	if len(caps.Plugins) != 1 {
		t.Fatal("reconnect lost registry")
	}
	callTool(t, httpClient, "mock_plugin_disable", args, nil)
	callTool(t, second, "mock_plugin_uninstall", args, nil)
	callTool(t, httpClient, "mock_capabilities", map[string]any{}, &caps)
	if len(caps.Plugins) != 0 {
		t.Fatal("uninstall was not shared")
	}
	duplicate := exec.CommandContext(ctx, bin, "serve", "--root", root, "--listen", "127.0.0.1:0")
	if out, err := duplicate.CombinedOutput(); err == nil || !strings.Contains(string(out), "DAEMON_EXISTS") {
		t.Fatalf("duplicate daemon: %v %s", err, out)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	other := t.TempDir()
	conflict := exec.CommandContext(ctx, bin, "serve", "--root", other, "--listen", occupied.Addr().String())
	if out, err := conflict.CombinedOutput(); err == nil {
		t.Fatalf("occupied port accepted: %s", out)
	}
	conn, err := net.DialTimeout("tcp", occupied.Addr().String(), time.Second)
	if err != nil {
		t.Fatal("occupied listener was disturbed", err)
	}
	conn.Close()
	if _, err = os.Stat(filepath.Join(other, ".x-mock/ready.json")); !os.IsNotExist(err) {
		t.Fatal("failed serve published readiness")
	}
	second.Close()
	reconnected.Close()
	httpClient.Close()
	stop()
	if _, err = os.Stat(readyPath); !os.IsNotExist(err) {
		t.Fatal("ready file remains after shutdown")
	}
}
