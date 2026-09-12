package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func pair(t *testing.T, handler Handler) (*Peer, *Peer) {
	t.Helper()
	a, b := net.Pipe()
	client := NewPeer(a, a, nil)
	server := NewPeer(b, b, handler)
	t.Cleanup(func() { client.Close(); server.Close() })
	return client, server
}

func TestCancellationWhileWriteIsBlockedReachesRemote(t *testing.T) {
	a, b := net.Pipe()
	client := NewPeer(a, a, nil)
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { var result string; done <- client.Call(ctx, "slow", struct{}{}, &result) }()
	// Wait for the request to enter the writer before cancelling its context.
	raw := make([]byte, 1)
	if _, err := b.Read(raw); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	cancelled := make(chan struct{})
	server := NewPeer(&prefixedConn{Conn: b, prefix: raw}, b, func(ctx context.Context, m string, p json.RawMessage) (any, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	defer server.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("request cancelled before write completion leaked remote handler")
	}
}

type prefixedConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixedConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

func TestOversizedAndMalformedFramesFailBoundedly(t *testing.T) {
	client, _ := pair(t, func(context.Context, string, json.RawMessage) (any, error) { return "ok", nil })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out string
	if err := client.Call(ctx, "large", map[string]string{"value": strings.Repeat("x", MaxMessage)}, &out); err == nil {
		t.Fatal("oversized IPC message accepted")
	}
	a, b := net.Pipe()
	bad := NewPeer(a, a, nil)
	defer bad.Close()
	defer b.Close()
	go func() { b.Write([]byte("{malformed}\n")) }()
	select {
	case <-bad.Done():
		if bad.Err() == nil {
			t.Fatal("missing framing error")
		}
	case <-ctx.Done():
		t.Fatal("malformed peer did not close")
	}
}
func TestOutOfOrder(t *testing.T) {
	first := make(chan struct{})
	release := make(chan struct{})
	client, _ := pair(t, func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "first" {
			close(first)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return map[string]string{"method": method}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var result map[string]string
		err := client.Call(ctx, "first", struct{}{}, &result)
		if err == nil && result["method"] != "first" {
			err = errors.New("wrong first result")
		}
		done <- err
	}()
	<-first
	var second map[string]string
	if err := client.Call(ctx, "second", struct{}{}, &second); err != nil {
		t.Fatal(err)
	}
	if second["method"] != "second" {
		t.Fatal(second)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestCancelDoesNotBlockOtherRequests(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	client, _ := pair(t, func(ctx context.Context, m string, p json.RawMessage) (any, error) {
		if m == "wait" {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		return "alive", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { var out string; done <- client.Call(ctx, "wait", struct{}{}, &out) }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("remote handler not cancelled")
	}
	var out string
	ctx2, c2 := context.WithTimeout(context.Background(), time.Second)
	defer c2()
	if err := client.Call(ctx2, "ping", struct{}{}, &out); err != nil || out != "alive" {
		t.Fatal(out, err)
	}
}
func TestEOFFinishesPendingAndPanicIsContained(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	client, server := pair(t, func(ctx context.Context, m string, p json.RawMessage) (any, error) {
		if m == "panic" {
			panic("plugin bug")
		}
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out any
	if err := client.Call(ctx, "panic", struct{}{}, &out); err == nil {
		t.Fatal("panic became success")
	}
	done := make(chan error, 1)
	go func() { done <- client.Call(ctx, "wait", struct{}{}, &out) }()
	<-entered
	server.Close()
	if err := <-done; err == nil {
		t.Fatal("closed IPC completed successfully")
	}
}
