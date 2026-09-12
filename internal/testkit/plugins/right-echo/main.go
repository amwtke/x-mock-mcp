package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"xmock.local/x-mock-mcp/pluginapi"
	"xmock.local/x-mock-mcp/pluginapi/host"
)

var pluginID = "echo-right"

type config struct {
	Prefix    string `json:"prefix"`
	FailStart bool   `json:"fail_start"`
}
type plugin struct {
	config     config
	instanceID string
}

func (p *plugin) Describe(context.Context) (pluginapi.Descriptor, error) {
	return pluginapi.Descriptor{Ref: pluginapi.Reference{ID: pluginID, Version: "0.1.0", Role: pluginapi.Right}, APIMajor: 1, Contracts: []pluginapi.Contract{{ID: "echo", Version: 1, Capabilities: []string{"echo"}}}, ConfigSchema: json.RawMessage(`{"type":"object","properties":{"prefix":{"type":"string"},"fail_start":{"type":"boolean"}},"additionalProperties":false}`), Features: []string{"scenario.prepare"}, Preparation: &pluginapi.PreparationContract{InputSchema: json.RawMessage(`{"type":"object"}`), CandidateSchema: json.RawMessage(`{"type":"object"}`), Guide: "Provide an opaque echo object."}}, nil
}
func (p *plugin) ValidateConfig(ctx context.Context, raw json.RawMessage) error {
	var c config
	return pluginapi.Decode(raw, &c)
}
func (p *plugin) Start(ctx context.Context, s pluginapi.InstanceSpec) (pluginapi.Ready, error) {
	if err := pluginapi.Decode(s.Config, &p.config); err != nil {
		return pluginapi.Ready{}, err
	}
	if p.config.FailStart {
		return pluginapi.Ready{}, pluginapi.Fail("INVALID_ARGUMENT", "requested fixture startup failure")
	}
	p.instanceID = s.InstanceID
	return pluginapi.Ready{InstanceID: s.InstanceID}, nil
}
func (p *plugin) Execute(ctx context.Context, q pluginapi.Request) (pluginapi.Decision, error) {
	var body struct {
		Value string `json:"value"`
	}
	if err := pluginapi.Decode(q.Payload, &body); err != nil {
		return pluginapi.Decision{}, err
	}
	out, _ := json.Marshal(map[string]string{"value": p.config.Prefix + body.Value, "instance_id": p.instanceID})
	return pluginapi.Decision{Kind: "completed", Payload: out}, nil
}
func (p *plugin) Complete(context.Context, pluginapi.Resolution) (json.RawMessage, error) {
	return nil, pluginapi.Fail("UNSUPPORTED", "echo has no generation requests")
}
func (p *plugin) Stop(context.Context) error { return nil }
func (p *plugin) Prepare(ctx context.Context, s pluginapi.PreparationSpec) (pluginapi.PreparationReport, error) {
	if len(s.Candidate) == 0 {
		return pluginapi.PreparationReport{Instructions: "Provide a candidate echo object."}, nil
	}
	a := sha256.Sum256(s.Input)
	b := sha256.Sum256(s.Candidate)
	return pluginapi.PreparationReport{Ready: true, CompiledBody: s.Candidate, InputDigest: hex.EncodeToString(a[:]), CompiledDigest: hex.EncodeToString(b[:])}, nil
}
func main() {
	if err := host.ServeRight(&plugin{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
