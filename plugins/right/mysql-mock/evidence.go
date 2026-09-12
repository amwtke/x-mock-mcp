package main

import (
	"encoding/json"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
)

func executionEvidence(raw json.RawMessage, execution mysqlv1.Execution) json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return raw
	}
	object["execution"] = mysqlv1.Encode(execution)
	return mysqlv1.Encode(object)
}
