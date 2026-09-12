package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"xmock.local/x-mock-mcp/internal/app"
	"xmock.local/x-mock-mcp/internal/binding"
	mcptransport "xmock.local/x-mock-mcp/internal/transport/mcp"
	"xmock.local/x-mock-mcp/pluginapi"
)

func command(args []string) (string, int, error) {
	if len(args) == 0 {
		return "", 0, pluginapi.Invalid("command required")
	}
	if args[0] == "capabilities" {
		return "mock_capabilities", 1, nil
	}
	if len(args) < 2 {
		return "", 0, pluginapi.Invalid("subcommand required")
	}
	key := args[0] + " " + args[1]
	names := map[string]string{"plugin install": "mock_plugin_install", "plugin enable": "mock_plugin_enable", "plugin disable": "mock_plugin_disable", "plugin uninstall": "mock_plugin_uninstall", "scenario prepare": "mock_scenario_prepare", "scenario put": "mock_scenario_put", "scenario export": "mock_scenario_export", "env create": "mock_environment_create", "env destroy": "mock_environment_destroy", "run start": "mock_run_start", "run status": "mock_run_status", "run trace": "mock_run_trace", "run stop": "mock_run_stop", "requests next": "mock_requests_next", "requests resolve": "mock_requests_resolve"}
	if name := names[key]; name != "" {
		return name, 2, nil
	}
	return "", 0, pluginapi.Invalid("unknown command %s", key)
}

type options struct {
	root, input, listen, config string
	args                        []string
}

func parse(args []string) (options, error) {
	root, err := os.Getwd()
	if err != nil {
		return options{}, err
	}
	o := options{root: root, listen: "127.0.0.1:18765", config: "x-mock.yaml"}
	for i := 0; i < len(args); i++ {
		v := args[i]
		if strings.HasPrefix(v, "--") {
			if i+1 >= len(args) {
				return o, pluginapi.Invalid("flag value required")
			}
			i++
			switch v {
			case "--root":
				o.root = args[i]
			case "--input":
				o.input = args[i]
			case "--listen":
				o.listen = args[i]
			case "--config":
				o.config = args[i]
			default:
				return o, pluginapi.Invalid("unknown flag %s", v)
			}
		} else {
			o.args = append(o.args, v)
		}
	}
	o.root, err = filepath.Abs(o.root)
	return o, err
}
func execute(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Println("x-mock serve --root PROJECT [--config x-mock.yaml] [--listen 127.0.0.1:18765]\nx-mock mcp stdio --root PROJECT\nx-mock capabilities|plugin ACTION|scenario ACTION|env ACTION|run ACTION|requests ACTION --root PROJECT --input request.json")
		return nil
	}
	o, err := parse(args)
	if err != nil {
		return err
	}
	if len(o.args) == 1 && o.args[0] == "serve" {
		return serve(ctx, o)
	}
	endpoint, token, err := readConnection(o.root)
	if err != nil {
		return err
	}
	if len(o.args) == 2 && o.args[0] == "mcp" && o.args[1] == "stdio" {
		return mcptransport.Bridge(ctx, endpoint, token)
	}
	name, used, err := command(o.args)
	if err != nil {
		return err
	}
	if used != len(o.args) {
		return pluginapi.Invalid("unexpected positional argument")
	}
	raw := json.RawMessage(`{}`)
	if o.input != "" {
		raw, err = os.ReadFile(o.input)
		if err != nil {
			return err
		}
	}
	client, err := mcptransport.Connect(ctx, endpoint, token)
	if err != nil {
		return err
	}
	defer client.Close()
	result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: raw})
	if err != nil {
		return err
	}
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return err
	}
	if result.IsError {
		return pluginapi.Fail("TOOL_FAILED", "see structured tool result")
	}
	return nil
}

type ready struct {
	PID        int    `json:"pid"`
	InstanceID string `json:"instance_id"`
	StartedAt  string `json:"started_at"`
	Address    string `json:"address"`
}

func readConnection(root string) (string, string, error) {
	var r ready
	if err := app.ReadJSON(filepath.Join(root, ".x-mock", "ready.json"), &r); err != nil {
		return "", "", fmt.Errorf("start x-mock serve for this project first: %w", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".x-mock", "token"))
	if err != nil {
		return "", "", err
	}
	return r.Address, strings.TrimSpace(string(raw)), nil
}
func readConfig(path string) (app.Config, error) {
	var config app.Config
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	var generic any
	if err = yaml.Unmarshal(raw, &generic); err != nil {
		return config, err
	}
	normalized, err := json.Marshal(generic)
	if err != nil {
		return config, err
	}
	err = pluginapi.Decode(normalized, &config)
	return config, err
}
func serve(ctx context.Context, o options) error {
	lease, err := app.AcquireDaemon(o.root)
	if err != nil {
		return err
	}
	defer lease.Close()
	configPath := o.config
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(o.root, configPath)
	}
	config, err := readConfig(configPath)
	if err != nil {
		return err
	}
	service, err := app.Open(o.root, config)
	if err != nil {
		return err
	}
	defer service.Close(context.Background())
	if err = service.Catalog.RecoverReferences(); err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(o.listen)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return pluginapi.Invalid("serve requires a loopback IP address")
	}
	listener, err := net.Listen("tcp", o.listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	dir := filepath.Join(o.root, ".x-mock")
	if err = os.WriteFile(filepath.Join(dir, "token"), []byte(token+"\n"), 0600); err != nil {
		return err
	}
	if err = os.Chmod(filepath.Join(dir, "token"), 0600); err != nil {
		return err
	}
	r := ready{PID: os.Getpid(), InstanceID: binding.ID(), StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Address: "http://" + listener.Addr().String() + "/mcp"}
	raw, _ := json.Marshal(r)
	temp, err := os.CreateTemp(dir, "ready-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	temp.Close()
	readyPath := filepath.Join(dir, "ready.json")
	if err = os.Rename(temp.Name(), readyPath); err != nil {
		return err
	}
	defer os.Remove(readyPath)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcptransport.HTTP(mcptransport.New(service), token, listener.Addr().String()))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Fprintf(os.Stderr, "X-Mock-MCP ready %s project=%s instance=%s\n", r.Address, service.Root, r.InstanceID)
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		service.Close(shutdown)
		return server.Shutdown(shutdown)
	}
}
