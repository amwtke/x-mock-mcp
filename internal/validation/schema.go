package validation

import (
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"reflect"
)

func SchemaFor[T any]() json.RawMessage {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{reflect.TypeFor[json.RawMessage](): {}}})
	if err != nil {
		panic(err)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	return raw
}
func Check(schemaRaw, input json.RawMessage) error {
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		return err
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return err
	}
	var value any
	if err = json.Unmarshal(input, &value); err != nil {
		return err
	}
	return resolved.Validate(value)
}
