package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/pluginapi"
)

func need(id string, deadline time.Time) pluginapi.Need {
	return pluginapi.Need{RequestID: id, Continuation: "continuation", DeadlineUnixMS: deadline.UnixMilli(), Schema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`), Context: json.RawMessage(`{}`)}
}
func TestLeaseResolutionWaitsForRightOutcomeAndIsIdempotent(t *testing.T) {
	q := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan json.RawMessage, 1)
	enqueued := make(chan struct{})
	go func() {
		close(enqueued)
		raw, err := q.Enqueue(ctx, "run", need("one", time.Now().Add(time.Minute)))
		if err == nil {
			received <- raw
		}
	}()
	<-enqueued
	batch, err := q.Next(ctx, NextRequest{RunID: "run", Limit: 1, WaitMS: 2000})
	if err != nil || len(batch.Requests) != 1 {
		t.Fatal(batch, err)
	}
	if _, err = q.Next(ctx, NextRequest{RunID: "run", Limit: 1}); pluginapi.Code(err) != "OWNER_CONFLICT" {
		t.Fatal(err)
	}
	r := ResolveRequest{RunID: "run", RequestID: "one", OwnerToken: batch.OwnerToken, LeaseToken: batch.Requests[0].LeaseToken, Candidate: json.RawMessage(`{"value":2}`)}
	invalid := r
	invalid.Candidate = json.RawMessage(`{"value":"wrong"}`)
	if _, err = q.Resolve(ctx, invalid); err == nil {
		t.Fatal("schema violation accepted")
	}
	ack := make(chan error, 1)
	go func() { _, err := q.Resolve(ctx, r); ack <- err }()
	select {
	case raw := <-received:
		if string(raw) != `{"value":2}` {
			t.Fatal(string(raw))
		}
	case <-time.After(time.Second):
		t.Fatal("candidate not delivered")
	}
	select {
	case err := <-ack:
		t.Fatal("premature ack", err)
	default:
	}
	q.ReportOutcome("one", json.RawMessage(`{"kind":"rows"}`), nil)
	if err = <-ack; err != nil {
		t.Fatal(err)
	}
	if _, err = q.Resolve(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Candidate = json.RawMessage(`{"value":3}`)
	if _, err = q.Resolve(ctx, r); pluginapi.Code(err) != "STATE_CONFLICT" {
		t.Fatal("conflicting retry", err)
	}
}
func TestConcurrentClaimTakeoverAndDeadline(t *testing.T) {
	clock := newFakeClock()
	q := New(clock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan error, 1)
	go func() { _, err := q.Enqueue(ctx, "run", need("one", clock.Now().Add(time.Minute))); ready <- err }()
	first, err := q.Next(ctx, NextRequest{RunID: "run", Limit: 1, WaitMS: 2000})
	if err != nil || len(first.Requests) != 1 {
		t.Fatal(first, err)
	}
	clock.Advance(16 * time.Second)
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := q.Next(ctx, NextRequest{RunID: "run", OwnerToken: first.OwnerToken, Limit: 1})
			if e == nil {
				claimed.Add(int32(len(b.Requests)))
			}
		}()
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatal("double lease")
	}
	next, err := q.Next(ctx, NextRequest{RunID: "run", Limit: 1, Takeover: true, ExpectedOwnerEpoch: first.OwnerEpoch})
	if err != nil || len(next.Requests) != 1 {
		t.Fatal(next, err)
	}
	if _, err = q.Resolve(ctx, ResolveRequest{RunID: "run", RequestID: "one", OwnerToken: first.OwnerToken, LeaseToken: first.Requests[0].LeaseToken, Candidate: json.RawMessage(`{"value":1}`)}); err == nil {
		t.Fatal("old owner accepted")
	}
	clock.Advance(time.Minute)
	if err = <-ready; pluginapi.Code(err) != "DEADLINE_EXCEEDED" {
		t.Fatal(err)
	}
	if _, err = q.Resolve(ctx, ResolveRequest{RunID: "run", RequestID: "one", OwnerToken: next.OwnerToken, LeaseToken: next.Requests[0].LeaseToken, Candidate: json.RawMessage(`{"value":1}`)}); err == nil {
		t.Fatal("late result accepted")
	}
}

func TestQueueBoundsCancellationAndRunIsolation(t *testing.T) {
	q := New(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 128; i++ {
		id := fmt.Sprintf("r-%03d", i)
		go q.Enqueue(ctx, "run", need(id, time.Now().Add(time.Minute)))
	}
	deadline := time.Now().Add(time.Second)
	for q.Pending("run") < 128 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if q.Pending("run") != 128 {
		t.Fatal("requests not queued")
	}
	if _, err := q.Enqueue(ctx, "run", need("full", time.Now().Add(time.Minute))); pluginapi.Code(err) != "QUEUE_FULL" {
		t.Fatal(err)
	}
	b, err := q.Next(ctx, NextRequest{RunID: "run", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	r := ResolveRequest{RunID: "another-run", RequestID: b.Requests[0].Need.RequestID, OwnerToken: b.OwnerToken, LeaseToken: b.Requests[0].LeaseToken, Candidate: json.RawMessage(`{"value":1}`)}
	if _, err = q.Resolve(ctx, r); pluginapi.Code(err) != "OWNER_CONFLICT" {
		t.Fatal(err)
	}
	q.CancelRun("run", pluginapi.Fail("CANCELLED", "test stopped"))
	if q.Pending("run") != 0 {
		t.Fatal("cancel leaked requests")
	}
	r.RunID = "run"
	if _, err = q.Resolve(ctx, r); err == nil {
		t.Fatal("cancelled request resolved")
	}
}
