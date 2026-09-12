package pluginapi

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (r Reference) Validate() error {
	if !idPattern.MatchString(r.ID) || !versionPattern.MatchString(r.Version) || (r.Role != Left && r.Role != Right) {
		return Invalid("invalid plugin reference")
	}
	return nil
}
func (r Reference) Key() string { return string(r.Role) + "/" + r.ID + "/" + r.Version }
func (d Descriptor) HasFeature(f string) bool {
	for _, v := range d.Features {
		if v == f {
			return true
		}
	}
	return false
}
func schemaObject(raw json.RawMessage) bool {
	var v map[string]any
	return len(raw) > 0 && json.Unmarshal(raw, &v) == nil && v != nil && v["type"] == "object"
}
func (d Descriptor) Validate() error {
	if err := d.Ref.Validate(); err != nil {
		return err
	}
	if d.APIMajor != 1 {
		return Fail("PLUGIN_API_MISMATCH", "expected plugin API major 1")
	}
	if !schemaObject(d.ConfigSchema) {
		return Invalid("config_schema must declare an object")
	}
	if len(d.ScenarioSchema) > 0 && !schemaObject(d.ScenarioSchema) {
		return Invalid("invalid scenario schema")
	}
	if len(d.Contracts) == 0 {
		return Invalid("plugin must declare a contract")
	}
	seen := map[string]bool{}
	for _, c := range d.Contracts {
		key := c.ID + "/" + jsonNumber(c.Version)
		if c.ID == "" || c.Version < 1 || seen[key] {
			return Invalid("invalid or duplicate contract")
		}
		seen[key] = true
		caps := map[string]bool{}
		for _, cap := range c.Capabilities {
			if cap == "" || caps[cap] {
				return Invalid("invalid capability")
			}
			caps[cap] = true
		}
	}
	features := map[string]bool{}
	for _, f := range d.Features {
		if f == "" || features[f] {
			return Invalid("duplicate or empty feature")
		}
		features[f] = true
		if strings.HasPrefix(f, "scenario.") && d.Ref.Role != Right {
			return Invalid("scenario extensions require a right plugin")
		}
	}
	if d.HasFeature("scenario.prepare") != (d.Preparation != nil) {
		return Invalid("preparation feature and contract must agree")
	}
	if d.Preparation != nil && (!schemaObject(d.Preparation.InputSchema) || !schemaObject(d.Preparation.CandidateSchema) || strings.TrimSpace(d.Preparation.Guide) == "") {
		return Invalid("preparation schemas and guide required")
	}
	return nil
}
func jsonNumber(n int) string { b, _ := json.Marshal(n); return string(b) }
func SafeRelative(name string) bool {
	return name != "" && name != "." && !strings.ContainsAny(name, "\\\x00:") && !path.IsAbs(name) && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../")
}
func ValidDigest(s string) bool { return digestPattern.MatchString(s) }
func (m Manifest) Validate() error {
	if err := m.Descriptor.Validate(); err != nil {
		return err
	}
	parts := strings.Split(m.Platform, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Invalid("platform must be GOOS/GOARCH")
	}
	if !SafeRelative(m.Entrypoint) {
		return Invalid("entrypoint must be a safe relative path")
	}
	if _, ok := m.Files[m.Entrypoint]; !ok {
		return Invalid("entrypoint missing from files")
	}
	for name, digest := range m.Files {
		if !SafeRelative(name) || name == "manifest.json" || !ValidDigest(digest) {
			return Invalid("invalid file entry %q", name)
		}
	}
	return nil
}
func (d Decision) Validate() error {
	count := 0
	if len(d.Payload) > 0 {
		count++
	}
	if d.Need != nil {
		count++
	}
	if d.Error != nil {
		count++
	}
	if count != 1 {
		return Invalid("decision must contain exactly one outcome")
	}
	switch d.Kind {
	case "completed":
		if len(d.Payload) == 0 || !json.Valid(d.Payload) || bytes.Equal(bytes.TrimSpace(d.Payload), []byte("null")) {
			return Invalid("completed needs payload")
		}
	case "needs_data":
		if d.Need == nil || d.Need.RequestID == "" || d.Need.Continuation == "" || d.Need.DeadlineUnixMS <= 0 || !schemaObject(d.Need.Schema) {
			return Invalid("needs_data requires a bounded typed continuation")
		}
	case "unsupported":
		if d.Error == nil || d.Error.Code == "" {
			return Invalid("unsupported requires failure")
		}
	default:
		return Invalid("unknown decision kind")
	}
	return nil
}
func (r PreparationReport) Validate() error {
	if r.Ready {
		if len(r.MissingInputs)+len(r.Ambiguities)+len(r.Unsupported)+len(r.Diagnostics) > 0 || !json.Valid(r.CompiledBody) || !ValidDigest(r.InputDigest) || !ValidDigest(r.CompiledDigest) {
			return Invalid("ready report is incomplete")
		}
	} else if len(r.CompiledBody) > 0 || r.CompiledDigest != "" {
		return Invalid("non-ready report cannot expose a compiled body")
	}
	return nil
}
func Decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(out); err != nil {
		return Invalid("invalid JSON: %v", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Invalid("expected one JSON value")
	}
	return nil
}
