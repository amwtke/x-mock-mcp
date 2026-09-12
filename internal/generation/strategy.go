package generation

import (
	"context"
	"encoding/json"
	"xmock.local/x-mock-mcp/pluginapi"
)

type StrictReplay struct{}

func (StrictReplay) Resolve(context.Context, pluginapi.Need) (json.RawMessage, error) {
	return nil, pluginapi.Fail("UNMATCHED_REQUEST", "strict replay has no matching materialized data")
}

type AgentFill struct {
	Queue *Queue
	RunID func() string
}

func (a AgentFill) Resolve(ctx context.Context, need pluginapi.Need) (json.RawMessage, error) {
	return a.Queue.Enqueue(ctx, a.RunID(), need)
}
