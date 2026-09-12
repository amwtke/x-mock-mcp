package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"time"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

type config struct {
	Mode string `json:"mode"`
}
type continuation struct {
	request         pluginapi.Request
	statement       Statement
	params          []mysqlv1.Value
	slot            Slot
	deadline        int64
	done            chan struct{}
	err             error
	candidateDigest string
	result          json.RawMessage
}
type plugin struct {
	mu         sync.Mutex
	store      *state
	bundle     Bundle
	config     config
	pending    map[string]*continuation
	slots      map[string]*continuation
	counts     map[string]int
	closed     map[string]bool
	violations []pluginapi.Failure
}

func identifier() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func (p *plugin) Describe(context.Context) (pluginapi.Descriptor, error) {
	return pluginapi.Descriptor{Ref: pluginapi.Reference{ID: "mysql-mock", Version: "0.2.0", Role: pluginapi.Right}, APIMajor: 1, Contracts: []pluginapi.Contract{{ID: "mysql.operation", Version: 1, Capabilities: []string{"query", "prepare", "bigint", "varchar", "writes", "transactions"}}}, ConfigSchema: mysqlv1.SchemaFor[config](), ScenarioSchema: mysqlv1.SchemaFor[Bundle](), Features: []string{"scenario.prepare", "scenario.export", "scenario.verify"}, Preparation: &pluginapi.PreparationContract{InputSchema: mysqlv1.SchemaFor[Input](), CandidateSchema: mysqlv1.SchemaFor[Bundle](), Guide: qaGuide}}, nil
}

//go:embed qa-guide.md
var qaGuide string

func (p *plugin) ValidateConfig(ctx context.Context, raw json.RawMessage) error {
	var c config
	if err := pluginapi.Decode(raw, &c); err != nil {
		return err
	}
	if c.Mode != "StrictReplay" && c.Mode != "AgentFill" {
		return pluginapi.Invalid("mode must be StrictReplay or AgentFill")
	}
	return nil
}
func (p *plugin) Start(ctx context.Context, spec pluginapi.InstanceSpec) (pluginapi.Ready, error) {
	if err := p.ValidateConfig(ctx, spec.Config); err != nil {
		return pluginapi.Ready{}, err
	}
	var c config
	pluginapi.Decode(spec.Config, &c)
	if spec.DataStrategy != "" && spec.DataStrategy != c.Mode {
		return pluginapi.Ready{}, pluginapi.Invalid("right mode must match environment data strategy")
	}
	var body Bundle
	if err := pluginapi.Decode(spec.Scenario, &body); err != nil {
		return pluginapi.Ready{}, err
	}
	if body.Replay.RequiresGeneration && c.Mode == "StrictReplay" {
		return pluginapi.Ready{}, pluginapi.Fail("UNMATCHED_REQUEST", "strict replay requires materialized initial reference entities")
	}
	for _, statement := range body.DatabaseScenario.Statements {
		compiled, err := Compile(statement, body.DatabaseScenario.Tables, body.DatabaseScenario.Database)
		if err != nil || !reflect.DeepEqual(compiled, statement.Plan) {
			return pluginapi.Ready{}, pluginapi.Invalid("compiled SQL plan mismatch")
		}
	}
	store, err := newState(body.DatabaseScenario)
	if err != nil {
		return pluginapi.Ready{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.store != nil {
		return pluginapi.Ready{}, pluginapi.Fail("STATE_CONFLICT", "already started")
	}
	p.store = store
	p.bundle = body
	p.config = c
	p.pending = map[string]*continuation{}
	p.slots = map[string]*continuation{}
	p.counts = map[string]int{}
	p.closed = map[string]bool{}
	p.violations = nil
	return pluginapi.Ready{InstanceID: spec.InstanceID}, nil
}
func (p *plugin) Stop(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, need := range p.pending {
		select {
		case <-need.done:
		default:
			need.err = pluginapi.Fail("CANCELLED", "plugin stopped")
			close(need.done)
		}
	}
	p.store = nil
	return nil
}
func outcome(raw json.RawMessage, err error) (pluginapi.Decision, error) {
	if err != nil {
		var db *mysqlFailure
		if errors.As(err, &db) {
			if db.Number == 1235 {
				return pluginapi.Decision{Kind: "unsupported", Error: &pluginapi.Failure{Code: "UNSUPPORTED", Message: db.Message}}, nil
			}
			return pluginapi.Decision{Kind: "completed", Payload: mysqlv1.Encode(mysqlv1.Error{Kind: "error", Number: db.Number, SQLState: db.SQLState, Message: db.Message})}, nil
		}
		return pluginapi.Decision{}, err
	}
	return pluginapi.Decision{Kind: "completed", Payload: raw}, nil
}
func (p *plugin) Execute(ctx context.Context, q pluginapi.Request) (decision pluginapi.Decision, resultErr error) {
	defer func() {
		var failure *pluginapi.Failure
		if resultErr != nil {
			failure = &pluginapi.Failure{Code: pluginapi.Code(resultErr), Message: resultErr.Error()}
		} else if decision.Kind == "unsupported" {
			failure = decision.Error
		} else {
			var result mysqlv1.Error
			if json.Unmarshal(decision.Payload, &result) == nil && result.Kind == "error" {
				failure = &pluginapi.Failure{Code: "DEPENDENCY_ERROR", Message: result.Message}
			}
		}
		if failure != nil {
			p.mu.Lock()
			if len(p.violations) < 16 {
				p.violations = append(p.violations, *failure)
			}
			p.mu.Unlock()
		}
	}()
	p.mu.Lock()
	s := p.store
	body := p.bundle
	cfg := p.config
	closed := p.closed[q.ConnectionID]
	p.mu.Unlock()
	if s == nil {
		return pluginapi.Decision{}, pluginapi.Fail("STATE_CONFLICT", "right not started")
	}
	if err := ctx.Err(); err != nil {
		return pluginapi.Decision{}, err
	}
	if closed && q.Operation != "connection.close" {
		return pluginapi.Decision{}, pluginapi.Fail("CANCELLED", "connection closed")
	}
	switch q.Operation {
	case "connection.open":
		var options struct {
			FoundRows bool `json:"found_rows"`
		}
		if len(q.Payload) > 0 {
			if err := pluginapi.Decode(q.Payload, &options); err != nil {
				return pluginapi.Decision{}, err
			}
		}
		s.mu.Lock()
		c := s.conn(q.ConnectionID)
		c.foundRows = options.FoundRows
		raw := okResult(c, 0, 0)
		s.mu.Unlock()
		return outcome(raw, nil)
	case "connection.close":
		p.mu.Lock()
		p.closed[q.ConnectionID] = true
		for _, need := range p.pending {
			if need.request.ConnectionID == q.ConnectionID {
				select {
				case <-need.done:
				default:
					need.err = pluginapi.Fail("CANCELLED", "connection closed")
					close(need.done)
				}
			}
		}
		s.closeConnection(q.ConnectionID)
		p.mu.Unlock()
		return outcome(json.RawMessage(`{"kind":"closed"}`), nil)
	case "database.use":
		var v struct {
			Database string `json:"database"`
		}
		if err := pluginapi.Decode(q.Payload, &v); err != nil {
			return pluginapi.Decision{}, err
		}
		if v.Database != body.DatabaseScenario.Database {
			return outcome(nil, dbError(1049, "42000", "unknown database"))
		}
		s.mu.Lock()
		raw := okResult(s.conn(q.ConnectionID), 0, 0)
		s.mu.Unlock()
		return outcome(raw, nil)
	case "statement.close", "statement.reset":
		var v struct {
			StatementID string `json:"statement_id"`
		}
		if err := pluginapi.Decode(q.Payload, &v); err != nil {
			return pluginapi.Decision{}, err
		}
		s.mu.Lock()
		c := s.conn(q.ConnectionID)
		_, exists := c.statements[v.StatementID]
		if q.Operation == "statement.close" {
			delete(c.statements, v.StatementID)
		}
		raw := okResult(c, 0, 0)
		s.mu.Unlock()
		if !exists {
			return outcome(nil, dbError(1243, "HY000", "unknown statement"))
		}
		return outcome(raw, nil)
	case "query", "statement.prepare", "statement.execute":
	default:
		return pluginapi.Decision{Kind: "unsupported", Error: &pluginapi.Failure{Code: "UNSUPPORTED", Message: "unsupported MySQL operation"}}, nil
	}
	var query mysqlv1.Query
	if err := pluginapi.Decode(q.Payload, &query); err != nil {
		return pluginapi.Decision{}, err
	}
	if query.Database != "" && query.Database != body.DatabaseScenario.Database {
		return outcome(nil, dbError(1049, "42000", "database mismatch"))
	}
	if q.Operation == "query" {
		if raw, known, err := s.control(ctx, q.ConnectionID, query.SQL); known {
			return outcome(raw, err)
		}
		if raw, known, err := s.system(ctx, q.ConnectionID, query.SQL, body.DatabaseScenario.Database); known {
			return outcome(raw, err)
		}
	}
	statement, params, err := matchStatement(body.DatabaseScenario, query.SQL, query.Params, q.Operation == "statement.prepare")
	if err != nil {
		return pluginapi.Decision{Kind: "unsupported", Error: &pluginapi.Failure{Code: "UNMATCHED_REQUEST", Message: err.Error()}}, nil
	}
	if q.Operation == "statement.prepare" {
		s.mu.Lock()
		c := s.conn(q.ConnectionID)
		c.nextStatement++
		id := strconv.FormatUint(c.nextStatement, 10)
		c.statements[id] = statement
		s.mu.Unlock()
		return outcome(mysqlv1.Encode(mysqlv1.Metadata{StatementID: id, Parameters: statement.Plan.Parameters, Columns: statement.Plan.Columns}), nil)
	}
	if q.Operation == "statement.execute" {
		s.mu.Lock()
		stored, ok := s.conn(q.ConnectionID).statements[query.StatementID]
		s.mu.Unlock()
		if !ok || stored.ID != statement.ID {
			return outcome(nil, dbError(1243, "HY000", "statement ownership mismatch"))
		}
	}
	p.mu.Lock()
	p.counts[statement.ID]++
	count := p.counts[statement.ID]
	expect, hasExpect := body.Verification.ExpectCalls[statement.ID]
	p.mu.Unlock()
	if hasExpect && count > expect.Max {
		return pluginapi.Decision{}, pluginapi.Fail("UNMATCHED_REQUEST", "statement count exceeded scenario maximum")
	}
	if body.DatabaseScenario.Mode == "fixture" {
		for _, c := range statement.Cases {
			if len(c.Params) != len(params) {
				continue
			}
			match := true
			for i, v := range params {
				match = match && equalValue(v, c.Params[i])
			}
			if match {
				s.mu.Lock()
				status := status(s.conn(q.ConnectionID))
				s.mu.Unlock()
				result := mysqlv1.Rows{Kind: "rows", Columns: statement.Plan.Columns, Rows: c.Rows, Status: status}
				if err = mysqlv1.ValidateRows(mysqlv1.Metadata{Columns: result.Columns}, result); err != nil {
					return pluginapi.Decision{}, err
				}
				return outcome(mysqlv1.Encode(result), nil)
			}
		}
		return pluginapi.Decision{}, pluginapi.Fail("UNMATCHED_REQUEST", "fixture parameters missing")
	}
	if statement.Plan.Kind == "select" {
		for _, slot := range body.DatabaseScenario.GenerationSlots {
			if slot.Table != statement.Plan.Table || slot.Materialized {
				continue
			}
			p.mu.Lock()
			existing := p.slots[slot.ID]
			if existing != nil {
				select {
				case <-existing.done:
					if existing.err == nil {
						p.mu.Unlock()
						continue
					}
					delete(p.slots, slot.ID)
					existing = nil
				default:
				}
			}
			if existing != nil && existing.deadline <= time.Now().UnixMilli() {
				existing.err = pluginapi.Fail("DEADLINE_EXCEEDED", "reference fill expired")
				close(existing.done)
				delete(p.slots, slot.ID)
				existing = nil
			}
			if existing != nil {
				done := existing.done
				p.mu.Unlock()
				select {
				case <-done:
					if existing.err != nil {
						return pluginapi.Decision{}, existing.err
					}
				case <-ctx.Done():
					return pluginapi.Decision{}, ctx.Err()
				}
				continue
			}
			if cfg.Mode != "AgentFill" {
				p.mu.Unlock()
				return pluginapi.Decision{}, pluginapi.Fail("UNMATCHED_REQUEST", "unmaterialized slot")
			}
			if len(p.pending) >= 128 {
				p.mu.Unlock()
				return pluginapi.Decision{}, pluginapi.Fail("QUEUE_FULL", "too many generation continuations")
			}
			token := identifier()
			need := &continuation{request: q, statement: statement, params: params, slot: slot, deadline: q.DeadlineUnixMS, done: make(chan struct{})}
			p.pending[token] = need
			p.slots[slot.ID] = need
			p.mu.Unlock()
			table, _ := findTable(body.DatabaseScenario.Tables, slot.Table)
			return pluginapi.Decision{Kind: "needs_data", Need: &pluginapi.Need{RequestID: q.ID, Continuation: token, StateVersion: 0, DeadlineUnixMS: q.DeadlineUnixMS, Schema: mysqlv1.SchemaFor[mysqlv1.EntityFill](), Context: mysqlv1.Encode(map[string]any{"slot": slot, "table": table, "qa": body.QAContract, "request": q, "statement_id": statement.ID})}}, nil
		}
	}
	return outcome(s.execute(ctx, q.ConnectionID, statement.Plan, params, statement.ID))
}
func (p *plugin) Complete(ctx context.Context, r pluginapi.Resolution) (raw json.RawMessage, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	need := p.pending[r.Continuation]
	if need == nil || need.request.ID != r.RequestID || r.StateVersion != 0 {
		return nil, pluginapi.Fail("STATE_CONFLICT", "continuation identity/version mismatch")
	}
	digest := sha256.Sum256(r.Payload)
	hash := hex.EncodeToString(digest[:])
	select {
	case <-need.done:
		if need.err == nil && need.candidateDigest == hash {
			return need.result, nil
		}
		return nil, pluginapi.Fail("STATE_CONFLICT", "continuation already terminated")
	default:
	}
	defer func() { need.err = err; need.result = raw; need.candidateDigest = hash; close(need.done) }()
	if p.closed[need.request.ConnectionID] {
		return nil, pluginapi.Fail("CANCELLED", "connection closed")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if time.Now().UnixMilli() >= need.deadline {
		return nil, pluginapi.Fail("DEADLINE_EXCEEDED", "late fill")
	}
	var fill mysqlv1.EntityFill
	if err = pluginapi.Decode(r.Payload, &fill); err != nil {
		return nil, err
	}
	if fill.SlotID != need.slot.ID || len(fill.Rows) != len(need.slot.Keys) {
		return nil, pluginapi.Fail("INVALID_RESULT", "reference slot or row count mismatch")
	}
	s := p.store
	if s == nil {
		return nil, pluginapi.Fail("CANCELLED", "right stopped")
	}
	s.mu.Lock()
	view := s.view(nil)
	table, _ := findTable(s.tables, need.slot.Table)
	keys := map[string]bool{}
	for _, k := range need.slot.Keys {
		keys[k] = true
	}
	for _, row := range fill.Rows {
		key, e := row[table.PrimaryKey[0]].String()
		if e != nil || !keys[key] {
			s.mu.Unlock()
			return nil, pluginapi.Fail("INVALID_RESULT", "unexpected or duplicate reference primary key")
		}
		delete(keys, key)
		if _, exists := view[table.Name][key]; exists {
			s.mu.Unlock()
			return nil, pluginapi.Fail("STATE_CONFLICT", "reference already exists")
		}
		for name, value := range need.slot.Fixed {
			if !equalValue(value, row[name]) {
				s.mu.Unlock()
				return nil, pluginapi.Fail("INVALID_RESULT", "reference violates fixed QA constraint")
			}
		}
		view[table.Name][key] = Entity(row)
	}
	if err = validateInitial(s.tables, asInitial(view)); err != nil {
		s.mu.Unlock()
		return nil, pluginapi.Fail("INVALID_RESULT", err.Error())
	}
	s.version++
	for _, row := range fill.Rows {
		key, _ := row[table.PrimaryKey[0]].String()
		s.committed[table.Name][key] = recordVersion{cloneRow(Entity(row)), s.version}
	}
	s.mu.Unlock()
	return s.execute(ctx, need.request.ConnectionID, need.statement.Plan, need.params, need.statement.ID)
}
