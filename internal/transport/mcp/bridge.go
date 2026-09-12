package mcptransport

import (
	"context"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func Bridge(ctx context.Context, endpoint, token string) error {
	remote, err := Connect(ctx, endpoint, token)
	if err != nil {
		return err
	}
	defer remote.Close()
	listed, err := remote.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	local := sdk.NewServer(&sdk.Implementation{Name: "x-mock-mcp-bridge", Version: "0.1.0"}, nil)
	for _, tool := range listed.Tools {
		description := *tool
		name := description.Name
		local.AddTool(&description, func(ctx context.Context, r *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return remote.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: r.Params.Arguments})
		})
	}
	return local.Run(ctx, &sdk.StdioTransport{})
}
