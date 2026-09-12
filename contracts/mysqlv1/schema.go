package mysqlv1

import (
	"encoding/json"
	"xmock.local/x-mock-mcp/internal/validation"
)

func SchemaFor[T any]() json.RawMessage               { return validation.SchemaFor[T]() }
func CheckSchema(schema, input json.RawMessage) error { return validation.Check(schema, input) }
