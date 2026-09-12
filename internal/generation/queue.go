package generation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"
	"xmock.local/x-mock-mcp/internal/binding"
	"xmock.local/x-mock-mcp/internal/validation"
	"xmock.local/x-mock-mcp/pluginapi"
)

type Clock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) func()
}
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, fn func()) func() {
	t := time.AfterFunc(d, fn)
	return func() { t.Stop() }
}

type NextRequest struct {
	RunID              string `json:"run_id"`
	OwnerToken         string `json:"owner_token,omitempty"`
	Limit              int    `json:"limit"`
	WaitMS             int    `json:"wait_ms,omitempty"`
	Takeover           bool   `json:"takeover,omitempty"`
	ExpectedOwnerEpoch int64  `json:"expected_owner_epoch,omitempty"`
}
type Leased struct {
	Need           pluginapi.Need `json:"need"`
	LeaseToken     string         `json:"lease_token"`
	LeaseExpiresMS int64          `json:"lease_expires_ms"`
}
type Batch struct {
	OwnerToken string   `json:"owner_token"`
	OwnerEpoch int64    `json:"owner_epoch"`
	Requests   []Leased `json:"requests"`
}
type ResolveRequest struct {
	RunID      string          `json:"run_id"`
	RequestID  string          `json:"request_id"`
	OwnerToken string          `json:"owner_token"`
	LeaseToken string          `json:"lease_token"`
	Candidate  json.RawMessage `json:"candidate"`
}
type Ack struct {
	RequestID string          `json:"request_id"`
	State     string          `json:"state"`
	Response  json.RawMessage `json:"response"`
}
type entry struct {
	run                string
	need               pluginapi.Need
	state, lease, hash string
	leaseExpiry        time.Time
	epoch              int64
	candidate          json.RawMessage
	delivery           chan json.RawMessage
	done               chan struct{}
	response           json.RawMessage
	err                error
	stop               func()
}
type owner struct {
	token string
	epoch int64
}
type Queue struct {
	mu      sync.Mutex
	clock   Clock
	entries map[string]*entry
	owners  map[string]owner
	changed chan struct{}
}

func New(clock Clock) *Queue {
	if clock == nil {
		clock = realClock{}
	}
	return &Queue{clock: clock, entries: map[string]*entry{}, owners: map[string]owner{}, changed: make(chan struct{})}
}
func (q *Queue) signal() { close(q.changed); q.changed = make(chan struct{}) }
func terminal(state string) bool {
	return state == "resolved" || state == "cancelled" || state == "expired" || state == "failed"
}
func (q *Queue) finish(e *entry, state string, raw json.RawMessage, err error) {
	if terminal(e.state) {
		return
	}
	e.state = state
	e.response = append(json.RawMessage(nil), raw...)
	e.err = err
	if e.stop != nil {
		e.stop()
	}
	close(e.done)
	q.signal()
}
func (q *Queue) Enqueue(ctx context.Context, run string, need pluginapi.Need) (json.RawMessage, error) {
	if run == "" || need.RequestID == "" || need.Continuation == "" || need.DeadlineUnixMS <= q.clock.Now().UnixMilli() {
		return nil, pluginapi.Fail("DEADLINE_EXCEEDED", "request needs an active run and future deadline")
	}
	q.mu.Lock()
	if _, ok := q.entries[need.RequestID]; ok {
		q.mu.Unlock()
		return nil, pluginapi.Fail("STATE_CONFLICT", "duplicate request ID")
	}
	active, own, total := 0, 0, 0
	for _, e := range q.entries {
		if e.run == run {
			total++
		}
		if !terminal(e.state) {
			active++
			if e.run == run {
				own++
			}
		}
	}
	if active >= 512 || own >= 128 || total >= 4096 {
		q.mu.Unlock()
		return nil, pluginapi.Fail("QUEUE_FULL", "generation queue capacity reached")
	}
	e := &entry{run: run, need: need, state: "pending", delivery: make(chan json.RawMessage, 1), done: make(chan struct{})}
	q.entries[need.RequestID] = e
	e.stop = q.clock.AfterFunc(time.UnixMilli(need.DeadlineUnixMS).Sub(q.clock.Now()), func() {
		q.CancelRequest(need.RequestID, pluginapi.Fail("DEADLINE_EXCEEDED", "generation deadline elapsed"))
	})
	q.signal()
	q.mu.Unlock()
	stopContext := context.AfterFunc(ctx, func() { q.CancelRequest(need.RequestID, pluginapi.Fail("CANCELLED", "application request cancelled")) })
	go func() { <-e.done; stopContext() }()
	select {
	case raw := <-e.delivery:
		return raw, nil
	case <-e.done:
		q.mu.Lock()
		err := e.err
		q.mu.Unlock()
		return nil, err
	case <-ctx.Done():
		q.CancelRequest(need.RequestID, pluginapi.Fail("CANCELLED", "application request cancelled"))
		return nil, ctx.Err()
	}
}
func (q *Queue) Next(ctx context.Context, r NextRequest) (Batch, error) {
	if r.RunID == "" || r.Limit < 1 || r.Limit > 8 || r.WaitMS < 0 || r.WaitMS > 2000 {
		return Batch{}, pluginapi.Invalid("run, limit 1..8 and wait_ms 0..2000 required")
	}
	wallDeadline := time.Now().Add(time.Duration(r.WaitMS) * time.Millisecond)
	q.mu.Lock()
	o, exists := q.owners[r.RunID]
	if r.Takeover {
		if !exists || o.epoch != r.ExpectedOwnerEpoch {
			q.mu.Unlock()
			return Batch{}, pluginapi.Fail("STATE_CONFLICT", "owner epoch changed")
		}
		for _, e := range q.entries {
			if e.run == r.RunID && e.state == "resolving" {
				q.mu.Unlock()
				return Batch{}, pluginapi.Fail("STATE_CONFLICT", "cannot take over an in-flight commit")
			}
		}
		o = owner{binding.ID(), o.epoch + 1}
		for _, e := range q.entries {
			if e.run == r.RunID && e.state == "leased" {
				e.state = "pending"
				e.lease = ""
			}
		}
		q.owners[r.RunID] = o
	} else if !exists {
		if r.OwnerToken != "" {
			q.mu.Unlock()
			return Batch{}, pluginapi.Fail("OWNER_CONFLICT", "unknown owner token")
		}
		o = owner{binding.ID(), 1}
		q.owners[r.RunID] = o
	} else if r.OwnerToken != o.token {
		q.mu.Unlock()
		return Batch{}, pluginapi.Fail("OWNER_CONFLICT", "run already has a generation owner")
	}
	for {
		current := q.owners[r.RunID]
		if current != o {
			q.mu.Unlock()
			return Batch{}, pluginapi.Fail("OWNER_CONFLICT", "generation owner changed")
		}
		now := q.clock.Now()
		out := Batch{OwnerToken: o.token, OwnerEpoch: o.epoch, Requests: []Leased{}}
		ids := []string{}
		for id, e := range q.entries {
			if e.run == r.RunID {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			e := q.entries[id]
			if terminal(e.state) {
				continue
			}
			deadline := time.UnixMilli(e.need.DeadlineUnixMS)
			if !now.Before(deadline) {
				q.finish(e, "expired", nil, pluginapi.Fail("DEADLINE_EXCEEDED", "generation deadline elapsed"))
				continue
			}
			if e.state == "leased" && !now.Before(e.leaseExpiry) {
				e.state = "pending"
				e.lease = ""
			}
			if e.state == "pending" && len(out.Requests) < r.Limit {
				e.state = "leased"
				e.lease = binding.ID()
				e.epoch = o.epoch
				e.leaseExpiry = now.Add(15 * time.Second)
				if deadline.Before(e.leaseExpiry) {
					e.leaseExpiry = deadline
				}
				out.Requests = append(out.Requests, Leased{Need: e.need, LeaseToken: e.lease, LeaseExpiresMS: e.leaseExpiry.UnixMilli()})
			}
		}
		if len(out.Requests) > 0 || r.WaitMS == 0 || !time.Now().Before(wallDeadline) {
			q.mu.Unlock()
			return out, nil
		}
		changed := q.changed
		q.mu.Unlock()
		timer := time.NewTimer(time.Until(wallDeadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			return Batch{}, ctx.Err()
		case <-changed:
			timer.Stop()
		case <-timer.C:
		}
		q.mu.Lock()
	}
}
func digest(raw json.RawMessage) string {
	var value any
	if pluginapi.Decode(raw, &value) != nil {
		return ""
	}
	normalized, _ := json.Marshal(value)
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}
func (q *Queue) Resolve(ctx context.Context, r ResolveRequest) (Ack, error) {
	q.mu.Lock()
	e := q.entries[r.RequestID]
	o := q.owners[r.RunID]
	if e == nil || e.run != r.RunID || o.token == "" || o.token != r.OwnerToken {
		q.mu.Unlock()
		return Ack{}, pluginapi.Fail("OWNER_CONFLICT", "request or owner mismatch")
	}
	hash := digest(r.Candidate)
	if hash == "" {
		q.mu.Unlock()
		return Ack{}, pluginapi.Invalid("invalid candidate JSON")
	}
	if e.hash != "" {
		if e.hash != hash || e.epoch != o.epoch || e.lease != r.LeaseToken {
			q.mu.Unlock()
			return Ack{}, pluginapi.Fail("STATE_CONFLICT", "result or lease conflicts with accepted candidate")
		}
		q.mu.Unlock()
		return q.wait(ctx, e)
	}
	now := q.clock.Now()
	if e.state != "leased" || e.lease != r.LeaseToken || e.epoch != o.epoch || !now.Before(e.leaseExpiry) || now.UnixMilli() >= e.need.DeadlineUnixMS {
		q.mu.Unlock()
		return Ack{}, pluginapi.Fail("STATE_CONFLICT", "lease expired, cancelled or does not match")
	}
	schema := append(json.RawMessage(nil), e.need.Schema...)
	q.mu.Unlock()
	if err := validation.Check(schema, r.Candidate); err != nil {
		return Ack{}, pluginapi.Fail("INVALID_RESULT", err.Error())
	}
	q.mu.Lock()
	now = q.clock.Now()
	if e.state != "leased" || q.owners[r.RunID] != o || e.lease != r.LeaseToken || !now.Before(e.leaseExpiry) || now.UnixMilli() >= e.need.DeadlineUnixMS {
		q.mu.Unlock()
		return Ack{}, pluginapi.Fail("STATE_CONFLICT", "lease changed during validation")
	}
	e.hash = hash
	e.candidate = append(json.RawMessage(nil), r.Candidate...)
	e.state = "resolving"
	e.delivery <- e.candidate
	q.signal()
	q.mu.Unlock()
	return q.wait(ctx, e)
}
func (q *Queue) wait(ctx context.Context, e *entry) (Ack, error) {
	select {
	case <-ctx.Done():
		return Ack{}, ctx.Err()
	case <-e.done:
		q.mu.Lock()
		defer q.mu.Unlock()
		return Ack{RequestID: e.need.RequestID, State: e.state, Response: append(json.RawMessage(nil), e.response...)}, e.err
	}
}
func (q *Queue) ReportOutcome(id string, raw json.RawMessage, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.entries[id]
	if e == nil || terminal(e.state) {
		return
	}
	if err != nil {
		q.finish(e, "failed", raw, err)
	} else if e.state == "resolving" {
		q.finish(e, "resolved", raw, nil)
	}
}
func (q *Queue) CancelRequest(id string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if e := q.entries[id]; e != nil {
		state := "cancelled"
		if pluginapi.Code(err) == "DEADLINE_EXCEEDED" {
			state = "expired"
		}
		q.finish(e, state, nil, err)
	}
}
func (q *Queue) CancelRun(run string, err error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.entries {
		if e.run == run {
			q.finish(e, "cancelled", nil, err)
		}
	}
}
func (q *Queue) Pending(run string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, e := range q.entries {
		if e.run == run && !terminal(e.state) {
			n++
		}
	}
	return n
}
func (q *Queue) Capture(run, id string) (pluginapi.Capture, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.entries[id]
	if e == nil || e.run != run || e.state != "resolved" {
		return pluginapi.Capture{}, pluginapi.Fail("INVALID_RESULT", "request is not a successful generated response")
	}
	return pluginapi.Capture{Candidate: append(json.RawMessage(nil), e.candidate...), GenerationContext: append(json.RawMessage(nil), e.need.Context...), Response: append(json.RawMessage(nil), e.response...)}, nil
}
func (q *Queue) Forget(run string) {
	q.CancelRun(run, pluginapi.Fail("CANCELLED", "run cleaned up"))
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, e := range q.entries {
		if e.run == run {
			delete(q.entries, id)
		}
	}
	delete(q.owners, run)
}
