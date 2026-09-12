package pluginapi

import (
	"encoding/json"
	"errors"
	"fmt"
)

type Failure struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (f *Failure) Error() string      { return f.Code + ": " + f.Message }
func Fail(code, message string) error { return &Failure{Code: code, Message: message} }
func Code(err error) string {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	if err == nil {
		return ""
	}
	return "INTERNAL"
}
func Invalid(format string, args ...any) error {
	return Fail("INVALID_ARGUMENT", fmt.Sprintf(format, args...))
}
