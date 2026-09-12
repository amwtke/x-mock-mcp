package main

import (
	"testing"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestCLICommandsAndRejectUnknown(t *testing.T) {
	for _, c := range [][]string{{"plugin", "install"}, {"scenario", "prepare"}, {"env", "destroy"}, {"run", "trace"}, {"requests", "resolve"}, {"capabilities"}} {
		if _, _, err := command(c); err != nil {
			t.Fatal(c, err)
		}
	}
	if _, _, err := command([]string{"run", "shell"}); pluginapi.Code(err) != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
}
