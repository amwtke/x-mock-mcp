package main

import (
	"fmt"
	"os"
	"xmock.local/x-mock-mcp/pluginapi/host"
)

func main() {
	if err := host.ServeLeft(&plugin{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
