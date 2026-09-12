package mcptransport

import (
	"context"
	"encoding/json"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/internal/app"
)

func TestToolContractsAndHTTPAuth(t *testing.T) {
	s, err := app.Open(t.TempDir(), app.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	server := New(s)
	httpServer := httptest.NewServer(HTTP(server, "test-token", ""))
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := Connect(ctx, httpServer.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	list, err := client.ListTools(ctx, nil)
	if err != nil || len(list.Tools) != 16 {
		t.Fatal(list, err)
	}
	for _, tool := range list.Tools {
		raw, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		json.Unmarshal(raw, &schema)
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatal(tool.Name, string(raw))
		}
		for name, property := range schema["properties"].(map[string]any) {
			if _, ok := property.(map[string]any); !ok {
				t.Fatalf("Claude Code requires object property schemas: %s.%s=%v", tool.Name, name, property)
			}
		}
	}
	result, err := client.CallTool(ctx, &sdk.CallToolParams{Name: "mock_capabilities", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatal(result, err)
	}
	result, err = client.CallTool(ctx, &sdk.CallToolParams{Name: "mock_capabilities", Arguments: map[string]any{"typo": true}})
	if err != nil || !result.IsError {
		t.Fatal("unknown root field accepted", result, err)
	}
	for _, token := range []string{"", "wrong"} {
		req, _ := http.NewRequest("POST", httpServer.URL, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 401 {
			t.Fatal(r.Status)
		}
	}
	req, _ := http.NewRequest("POST", httpServer.URL, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Origin", "https://unrelated.example")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("origin", r.Status)
	}
}
func TestOldAndNewProtocolTools(t *testing.T) {
	s, _ := app.Open(t.TempDir(), app.Config{})
	defer s.Close(context.Background())
	server := httptest.NewServer(HTTP(New(s), "token", ""))
	defer server.Close()
	for _, version := range []string{"2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			raw := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
			if version == "2026-07-28" {
				raw = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
			}
			req, _ := http.NewRequest("POST", server.URL, strings.NewReader(raw))
			req.Header.Set("Authorization", "Bearer token")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", version)
			if version == "2026-07-28" {
				req.Header.Set("Mcp-Method", "tools/list")
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != 200 || !strings.Contains(string(body), "mock_scenario_prepare") {
				t.Fatal(res.Status, string(body))
			}
		})
	}
}
