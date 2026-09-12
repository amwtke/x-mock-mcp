package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"xmock.local/x-mock-mcp/pluginapi/host"
)

func main() {
	p := &plugin{}
	if len(os.Args) == 3 && os.Args[1] == "--emit-schemas" {
		desc, err := p.Describe(context.Background())
		if err != nil {
			panic(err)
		}
		dir := os.Args[2]
		if err = os.MkdirAll(dir, 0755); err != nil {
			panic(err)
		}
		for name, raw := range map[string]json.RawMessage{"qa-input.schema.json": desc.Preparation.InputSchema, "candidate.schema.json": desc.Preparation.CandidateSchema, "scenario.schema.json": desc.ScenarioSchema, "config.schema.json": desc.ConfigSchema} {
			var value any
			if err = json.Unmarshal(raw, &value); err != nil {
				panic(err)
			}
			pretty, _ := json.MarshalIndent(value, "", "  ")
			if err = os.WriteFile(filepath.Join(dir, name), append(pretty, '\n'), 0644); err != nil {
				panic(err)
			}
		}
		if err = os.WriteFile(filepath.Join(dir, "qa-guide.md"), []byte(strings.TrimRight(qaGuide, "\n")+"\n"), 0644); err != nil {
			panic(err)
		}
		return
	}
	if err := host.ServeRight(p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
