package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"xmock.local/x-mock-mcp/internal/binding"
	"xmock.local/x-mock-mcp/internal/generation"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	pluginruntime "xmock.local/x-mock-mcp/internal/plugin/runtime"
	jobrun "xmock.local/x-mock-mcp/internal/run"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/internal/trace"
	"xmock.local/x-mock-mcp/internal/validation"
	"xmock.local/x-mock-mcp/pluginapi"
)

type Config struct {
	Jobs []jobrun.JobSpec `json:"jobs"`
}
type BindingRequest struct {
	ResourceID  string              `json:"resource_id"`
	Left        pluginapi.Reference `json:"left"`
	Right       pluginapi.Reference `json:"right"`
	Contract    pluginapi.Contract  `json:"contract"`
	LeftConfig  json.RawMessage     `json:"left_config"`
	RightConfig json.RawMessage     `json:"right_config"`
}
type EnvironmentRequest struct {
	Bindings        []BindingRequest `json:"bindings"`
	ScenarioID      string           `json:"scenario_id"`
	ScenarioVersion int64            `json:"scenario_version"`
	DataStrategy    string           `json:"data_strategy"`
	TimeoutMS       int64            `json:"timeout_ms"`
}
type Environment struct {
	ID           string            `json:"environment_id"`
	Bindings     []binding.Binding `json:"bindings"`
	Snapshot     scenario.Snapshot `json:"snapshot"`
	DataStrategy string            `json:"data_strategy"`
}
type environment struct {
	mu        sync.Mutex
	view      Environment
	runID     string
	destroyed bool
	failure   error
	requests  map[string]pluginapi.Request
}
type Service struct {
	Root      string
	Catalog   *catalog.Catalog
	Factory   *pluginruntime.Factory
	Bindings  *binding.Manager
	Scenarios *scenario.Store
	Trace     *trace.Store
	Queue     *generation.Queue
	Runs      *jobrun.Manager
	mu        sync.Mutex
	envs      map[string]*environment
	jobs      map[string]jobrun.JobSpec
}

func Open(root string, config Config) (*Service, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	s := &Service{Root: root, envs: map[string]*environment{}, jobs: map[string]jobrun.JobSpec{}, Queue: generation.New(nil)}
	for _, job := range config.Jobs {
		if job.Name == "" || len(job.Argv) == 0 {
			return nil, pluginapi.Invalid("named job requires argv")
		}
		if _, ok := s.jobs[job.Name]; ok {
			return nil, pluginapi.Invalid("duplicate job name")
		}
		s.jobs[job.Name] = job
	}
	s.Catalog, err = catalog.Open(root)
	if err != nil {
		return nil, err
	}
	s.Factory = pluginruntime.New(s.Catalog)
	s.Bindings = binding.New(s.Factory)
	s.Scenarios, err = scenario.Open(root, s.ValidateDocument)
	if err != nil {
		return nil, err
	}
	s.Trace, err = trace.Open(root, 64<<20)
	if err != nil {
		return nil, err
	}
	s.Runs, err = jobrun.New(root)
	return s, err
}
func (s *Service) Prepare(ctx context.Context, ref pluginapi.Reference, input, candidate json.RawMessage) (pluginapi.PreparationReport, error) {
	if ref.Role != pluginapi.Right {
		return pluginapi.PreparationReport{}, pluginapi.Invalid("preparation belongs to right plugins")
	}
	return s.Factory.Prepare(ctx, ref, pluginapi.PreparationSpec{ProjectRoot: s.Root, Input: input, Candidate: candidate})
}
func (s *Service) ValidateDocument(ctx context.Context, d scenario.Document) (scenario.Document, error) {
	ref := pluginapi.Reference{ID: d.PluginID, Version: d.PluginVersion, Role: pluginapi.Right}
	manifest, _, err := s.Catalog.Package(ref)
	if err != nil {
		return d, err
	}
	desc := manifest.Descriptor
	found := false
	for _, c := range desc.Contracts {
		if c.ID == d.ContractID && c.Version == d.ContractVersion {
			found = true
		}
	}
	if !found {
		return d, pluginapi.Fail("CONTRACT_MISMATCH", "scenario contract is not supported by right plugin")
	}
	if desc.HasFeature("scenario.prepare") {
		report, err := s.Prepare(ctx, ref, d.Input, d.Body)
		if err != nil {
			return d, err
		}
		if !report.Ready {
			code := "INVALID_SCENARIO"
			for _, issue := range report.Diagnostics {
				if strings.Contains(issue.Message, "STALE_EVIDENCE") {
					code = "STALE_EVIDENCE"
				}
			}
			raw, _ := json.Marshal(report)
			return d, &pluginapi.Failure{Code: code, Message: "right plugin rejected scenario preparation", Details: raw}
		}
		d.Body = report.CompiledBody
	}
	if err = validation.Check(desc.ScenarioSchema, d.Body); err != nil {
		return d, pluginapi.Fail("INVALID_SCENARIO", err.Error())
	}
	return d, nil
}
func (s *Service) CreateEnvironment(ctx context.Context, r EnvironmentRequest) (out Environment, err error) {
	if r.DataStrategy != "AgentFill" && r.DataStrategy != "StrictReplay" {
		return out, pluginapi.Invalid("data_strategy must be AgentFill or StrictReplay")
	}
	if len(r.Bindings) < 1 || len(r.Bindings) > 8 || r.TimeoutMS < 1 || r.TimeoutMS > 300000 {
		return out, pluginapi.Invalid("1..8 bindings and timeout_ms 1..300000 required")
	}
	records, err := s.Catalog.Records()
	if err != nil {
		return out, err
	}
	digests := map[string]string{}
	for _, b := range r.Bindings {
		for _, ref := range []pluginapi.Reference{b.Left, b.Right} {
			ok := false
			for _, record := range records {
				if record.Ref == ref && record.Enabled {
					ok = true
					digests[ref.Key()] = record.ArchiveSHA256
				}
			}
			if !ok {
				return out, pluginapi.Fail("PLUGIN_NOT_ENABLED", "binding plugin must be installed and enabled")
			}
		}
	}
	snapshot, err := s.Scenarios.Freeze(ctx, r.ScenarioID, r.ScenarioVersion, digests)
	if err != nil {
		return out, err
	}
	e := &environment{view: Environment{ID: binding.ID(), Snapshot: snapshot, DataStrategy: r.DataStrategy, Bindings: []binding.Binding{}}, requests: map[string]pluginapi.Request{}}
	resources := map[string]bool{}
	success := false
	defer func() {
		if !success {
			for _, b := range e.view.Bindings {
				s.Bindings.Destroy(context.Background(), b.ID)
			}
		}
	}()
	for _, b := range r.Bindings {
		if b.Right.ID != snapshot.Document.PluginID || b.Right.Version != snapshot.Document.PluginVersion || b.Contract.ID != snapshot.Document.ContractID || b.Contract.Version != snapshot.Document.ContractVersion {
			return out, pluginapi.Fail("CONTRACT_MISMATCH", "binding must match frozen scenario plugin and contract")
		}
		if !scenario.ValidID(b.ResourceID) || resources[b.ResourceID] {
			return out, pluginapi.Invalid("resource ID must be unique")
		}
		resources[b.ResourceID] = true
		var strategy pluginapi.DataStrategy = generation.StrictReplay{}
		if r.DataStrategy == "AgentFill" {
			strategy = generation.AgentFill{Queue: s.Queue, RunID: func() string { e.mu.Lock(); defer e.mu.Unlock(); return e.runID }}
		}
		configured := binding.Config{Left: b.Left, Right: b.Right, Contract: b.Contract, EnvironmentID: e.view.ID, ResourceID: b.ResourceID, LeftConfig: b.LeftConfig, RightConfig: b.RightConfig, Scenario: snapshot.Document.Body, ScenarioVersion: snapshot.Document.Version, RequestTimeoutMS: r.TimeoutMS, DataStrategy: strategy, DataStrategyName: r.DataStrategy, OutcomeObserver: func(q pluginapi.Request, raw json.RawMessage, err error) { s.observe(e, q, raw, err) }, OnFailure: func(err error) {
			e.mu.Lock()
			e.failure = err
			run := e.runID
			e.mu.Unlock()
			if run != "" {
				s.Queue.CancelRun(run, err)
				s.Runs.Abort(run, err)
			}
		}}
		actual, err := s.Bindings.Create(ctx, configured)
		if err != nil {
			return out, err
		}
		e.view.Bindings = append(e.view.Bindings, actual)
	}
	s.mu.Lock()
	if len(s.envs) >= 16 {
		s.mu.Unlock()
		return out, pluginapi.Fail("QUEUE_FULL", "environment capacity reached")
	}
	s.envs[e.view.ID] = e
	s.mu.Unlock()
	success = true
	return e.view, nil
}
func (s *Service) observe(e *environment, q pluginapi.Request, raw json.RawMessage, err error) {
	s.Queue.ReportOutcome(q.ID, raw, err)
	if q.RunID == "" {
		return
	}
	event := trace.Event{Kind: "dependency.result", Request: &q, Payload: raw}
	if q.StartedUnixMS > 0 {
		event.DurationMS = time.Now().UnixMilli() - q.StartedUnixMS
	}
	if err != nil {
		event.Error = &pluginapi.Failure{Code: pluginapi.Code(err), Message: err.Error()}
	}
	e.mu.Lock()
	if err != nil && e.failure == nil {
		e.failure = err
	}
	e.requests[q.ID] = q
	e.mu.Unlock()
	if _, traceErr := s.Trace.Append(q.RunID, event); traceErr != nil {
		e.mu.Lock()
		e.failure = traceErr
		e.mu.Unlock()
		s.Queue.CancelRun(q.RunID, traceErr)
		s.Runs.Abort(q.RunID, traceErr)
	}
	if capture, captureErr := s.Queue.Capture(q.RunID, q.ID); captureErr == nil {
		capture.Request = q
		payload, _ := json.Marshal(capture)
		if _, traceErr := s.Trace.Append(q.RunID, trace.Event{Kind: "generation.confirmed", Payload: payload}); traceErr != nil {
			e.mu.Lock()
			e.failure = traceErr
			e.mu.Unlock()
			s.Queue.CancelRun(q.RunID, traceErr)
			s.Runs.Abort(q.RunID, traceErr)
		}
	}
}
func (s *Service) getEnvironment(id string) (*environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.envs[id]
	if e == nil {
		return nil, pluginapi.Fail("NOT_FOUND", "environment does not exist")
	}
	return e, nil
}
func (s *Service) StartRun(environmentID, jobName string) (jobrun.Status, error) {
	e, err := s.getEnvironment(environmentID)
	if err != nil {
		return jobrun.Status{}, err
	}
	job, ok := s.jobs[jobName]
	if !ok {
		return jobrun.Status{}, pluginapi.Fail("NOT_FOUND", "job is not in project configuration")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyed || e.runID != "" {
		return jobrun.Status{}, pluginapi.Fail("STATE_CONFLICT", "each environment supports one run; create a fresh environment")
	}
	id := binding.ID()
	for _, b := range e.view.Bindings {
		if err = s.Bindings.AssignRun(b.ID, id); err != nil {
			return jobrun.Status{}, err
		}
	}
	e.runID = id
	replacements := []string{"${project_root}", s.Root}
	for _, b := range e.view.Bindings {
		for key, value := range b.Endpoints {
			replacements = append(replacements, "${endpoint."+b.ResourceID+"."+key+"}", value)
		}
	}
	replace := strings.NewReplacer(replacements...)
	env := map[string]string{}
	for key, value := range job.Env {
		env[key] = replace.Replace(value)
		if strings.Contains(env[key], "${") {
			return jobrun.Status{}, pluginapi.Invalid("unresolved job endpoint variable")
		}
	}
	job.Env = env
	snapshot, _ := json.Marshal(e.view.Snapshot)
	if _, err = s.Trace.Append(id, trace.Event{Kind: "run.snapshot", Payload: snapshot}); err != nil {
		return jobrun.Status{}, err
	}
	return s.Runs.Start(id, environmentID, job, map[string]string{"X_MOCK_RUN_ID": id}, func(ctx context.Context, status jobrun.Status) error { return s.finishRun(ctx, e, status) })
}
func (s *Service) finishRun(ctx context.Context, e *environment, status jobrun.Status) error {
	pending := s.Queue.Pending(status.ID)
	s.Queue.CancelRun(status.ID, pluginapi.Fail("CANCELLED", "test job ended"))
	e.mu.Lock()
	failure := e.failure
	e.mu.Unlock()
	defer func() {
		for _, b := range e.view.Bindings {
			s.Bindings.Seal(b.ID)
		}
	}()
	if failure != nil {
		return failure
	}
	if pending > 0 {
		return pluginapi.Fail("EXPECTATION_FAILED", "test ended with unresolved requests")
	}
	for _, b := range e.view.Bindings {
		right, err := s.Bindings.Right(b.ID)
		if err != nil {
			return err
		}
		desc, err := right.Describe(ctx)
		if err != nil {
			return err
		}
		if desc.HasFeature("scenario.verify") {
			verifier, ok := right.(pluginapi.ScenarioVerifier)
			if !ok {
				return pluginapi.Fail("UNSUPPORTED", "right verification unavailable")
			}
			report, err := verifier.Verify(ctx)
			payload, _ := json.Marshal(report)
			if _, traceErr := s.Trace.Append(status.ID, trace.Event{Kind: "scenario.verification", Payload: payload}); traceErr != nil {
				return traceErr
			}
			if err != nil {
				return err
			}
			if !report.Passed {
				return pluginapi.Fail("EXPECTATION_FAILED", fmt.Sprint(report.Issues))
			}
		}
	}
	return nil
}
func (s *Service) Export(ctx context.Context, runID string, ids []string) (scenario.Document, error) {
	status, err := s.Runs.Status(runID)
	if err != nil {
		return scenario.Document{}, err
	}
	if status.State != "succeeded" || len(ids) == 0 || len(ids) > 128 {
		return scenario.Document{}, pluginapi.Fail("STATE_CONFLICT", "export requires a successful run and explicit generated request IDs")
	}
	e, err := s.getEnvironment(status.EnvironmentID)
	if err != nil {
		return scenario.Document{}, err
	}
	if len(e.view.Bindings) != 1 {
		return scenario.Document{}, pluginapi.Fail("UNSUPPORTED", "P0 export requires one resource")
	}
	captures := []pluginapi.Capture{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return scenario.Document{}, pluginapi.Invalid("duplicate capture")
		}
		seen[id] = true
		c, err := s.Queue.Capture(runID, id)
		if err != nil {
			return scenario.Document{}, err
		}
		e.mu.Lock()
		c.Request = e.requests[id]
		e.mu.Unlock()
		captures = append(captures, c)
	}
	right, err := s.Bindings.Right(e.view.Bindings[0].ID)
	if err != nil {
		return scenario.Document{}, err
	}
	exporter, ok := right.(pluginapi.ScenarioExporter)
	if !ok {
		return scenario.Document{}, pluginapi.Fail("UNSUPPORTED", "right cannot export")
	}
	body, err := exporter.Export(ctx, pluginapi.ExportSpec{Snapshot: e.view.Snapshot.Document.Body, Captures: captures})
	if err != nil {
		return scenario.Document{}, err
	}
	candidate := e.view.Snapshot.Document
	candidate.Version = 0
	candidate.Body = body
	return s.ValidateDocument(ctx, candidate)
}
func (s *Service) DestroyEnvironment(ctx context.Context, id string) error {
	e, err := s.getEnvironment(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.destroyed = true
	runID := e.runID
	e.mu.Unlock()
	if runID != "" {
		s.Queue.CancelRun(runID, pluginapi.Fail("CANCELLED", "environment destroyed"))
		s.Runs.Stop(runID)
		if err = s.Runs.Wait(ctx, runID); err != nil && pluginapi.Code(err) != "NOT_FOUND" {
			return err
		}
	}
	for _, b := range e.view.Bindings {
		if err = s.Bindings.Destroy(ctx, b.ID); err != nil {
			return err
		}
	}
	if runID != "" {
		s.Queue.Forget(runID)
	}
	s.mu.Lock()
	delete(s.envs, id)
	s.mu.Unlock()
	return nil
}
func (s *Service) Close(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
	}
	s.mu.Lock()
	ids := []string{}
	for id := range s.envs {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		if err := s.DestroyEnvironment(ctx, id); err != nil {
			return err
		}
	}
	return s.Runs.Close(ctx)
}
func (s *Service) Jobs() []string {
	out := []string{}
	for name := range s.jobs {
		out = append(out, name)
	}
	return out
}
func ReadJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return pluginapi.Decode(raw, v)
}
