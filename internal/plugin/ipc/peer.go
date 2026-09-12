package ipc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"xmock.local/x-mock-mcp/pluginapi"
)

const MaxMessage = 4 << 20

type Handler func(context.Context, string, json.RawMessage) (any, error)
type rpcError struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *pluginapi.Failure `json:"data,omitempty"`
}
type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type answer struct {
	result json.RawMessage
	err    error
}
type writeJob struct {
	data []byte
	done chan error
}
type Peer struct {
	reader  io.ReadCloser
	writer  io.WriteCloser
	handler Handler
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	pending map[string]chan answer
	running map[string]context.CancelFunc
	done    chan struct{}
	once    sync.Once
	err     error
	writes  chan writeJob
	slots   chan struct{}
	prefix  string
	seq     atomic.Uint64
}

func NewPeer(reader io.ReadCloser, writer io.WriteCloser, handler Handler) *Peer {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	p := &Peer{reader: reader, writer: writer, handler: handler, pending: map[string]chan answer{}, running: map[string]context.CancelFunc{}, done: make(chan struct{}), writes: make(chan writeJob, 128), slots: make(chan struct{}, 32), prefix: hex.EncodeToString(id)}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	go p.writeLoop()
	go p.readLoop()
	return p
}
func (p *Peer) Done() <-chan struct{} { return p.done }
func (p *Peer) Err() error            { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Peer) Close() error          { p.shutdown(io.EOF); return nil }
func (p *Peer) shutdown(err error) {
	p.once.Do(func() {
		p.cancel()
		p.mu.Lock()
		p.err = err
		for id, ch := range p.pending {
			ch <- answer{err: err}
			delete(p.pending, id)
		}
		for _, cancel := range p.running {
			cancel()
		}
		clear(p.running)
		close(p.done)
		p.mu.Unlock()
		p.reader.Close()
		p.writer.Close()
	})
}
func (p *Peer) Call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("%s-%d", p.prefix, p.seq.Add(1))
	ch := make(chan answer, 1)
	p.mu.Lock()
	if p.err != nil {
		err = p.err
		p.mu.Unlock()
		return err
	}
	if len(p.pending) >= 128 {
		p.mu.Unlock()
		return pluginapi.Fail("QUEUE_FULL", "too many pending IPC requests")
	}
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	if err = p.send(ctx, frame{JSONRPC: "2.0", ID: id, Method: method, Params: raw}); err != nil {
		if ctx.Err() != nil {
			p.cancelRequest(id)
		}
		return err
	}
	select {
	case response := <-ch:
		if response.err != nil {
			return response.err
		}
		if result == nil {
			return nil
		}
		return pluginapi.Decode(response.result, result)
	case <-ctx.Done():
		p.cancelRequest(id)
		return ctx.Err()
	case <-p.done:
		return p.Err()
	}
}
func (p *Peer) cancelRequest(id string) {
	raw, _ := json.Marshal(map[string]string{"request_id": id})
	p.enqueue(frame{JSONRPC: "2.0", Method: "cancel", Params: raw})
}
func (p *Peer) Notify(ctx context.Context, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return p.send(ctx, frame{JSONRPC: "2.0", Method: method, Params: raw})
}
func encode(f frame) ([]byte, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxMessage {
		return nil, pluginapi.Fail("INVALID_ARGUMENT", "IPC message exceeds 4 MiB")
	}
	return append(b, '\n'), nil
}
func (p *Peer) send(ctx context.Context, f frame) error {
	b, err := encode(f)
	if err != nil {
		return err
	}
	job := writeJob{data: b, done: make(chan error, 1)}
	select {
	case p.writes <- job:
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return p.Err()
	}
	select {
	case err := <-job.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return p.Err()
	}
}
func (p *Peer) enqueue(f frame) {
	b, err := encode(f)
	if err != nil {
		p.shutdown(err)
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	select {
	case p.writes <- writeJob{data: b, done: make(chan error, 1)}:
	default:
		p.shutdown(pluginapi.Fail("QUEUE_FULL", "IPC output queue exhausted"))
	}
}
func (p *Peer) writeLoop() {
	for {
		select {
		case <-p.done:
			return
		case job := <-p.writes:
			_, err := io.Copy(p.writer, bytes.NewReader(job.data))
			job.done <- err
			if err != nil {
				p.shutdown(err)
				return
			}
		}
	}
}

func (p *Peer) readLoop() {
	scanner := bufio.NewScanner(p.reader)
	scanner.Buffer(make([]byte, 64<<10), MaxMessage+1)
	for scanner.Scan() {
		var f frame
		if err := pluginapi.Decode(scanner.Bytes(), &f); err != nil {
			p.shutdown(err)
			return
		}
		if f.JSONRPC != "2.0" {
			p.shutdown(pluginapi.Invalid("invalid IPC jsonrpc version"))
			return
		}
		if f.Method != "" {
			if len(f.Result) > 0 || f.Error != nil {
				p.shutdown(pluginapi.Invalid("IPC frame mixes request and response"))
				return
			}
			if f.Method == "cancel" && f.ID == "" {
				var params struct {
					RequestID string `json:"request_id"`
				}
				if pluginapi.Decode(f.Params, &params) == nil {
					p.mu.Lock()
					cancel := p.running[params.RequestID]
					p.mu.Unlock()
					if cancel != nil {
						cancel()
					}
				}
				continue
			}
			p.startHandler(f)
		} else {
			if f.ID == "" || len(f.Params) > 0 || ((len(f.Result) > 0) == (f.Error != nil)) {
				p.shutdown(pluginapi.Invalid("invalid IPC response"))
				return
			}
			p.mu.Lock()
			ch := p.pending[f.ID]
			delete(p.pending, f.ID)
			p.mu.Unlock()
			if ch != nil {
				a := answer{result: f.Result}
				if f.Error != nil {
					a.err = f.Error.Data
					if f.Error.Data == nil {
						a.err = pluginapi.Fail("IPC_ERROR", f.Error.Message)
					}
				}
				ch <- a
			}
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	p.shutdown(err)
}
func (p *Peer) startHandler(f frame) {
	select {
	case p.slots <- struct{}{}:
	default:
		if f.ID != "" {
			p.respond(f.ID, nil, pluginapi.Fail("QUEUE_FULL", "too many concurrent IPC handlers"))
		}
		return
	}
	ctx, cancel := context.WithCancel(p.ctx)
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		cancel()
		<-p.slots
		return
	}
	if f.ID != "" {
		if _, exists := p.running[f.ID]; exists {
			p.mu.Unlock()
			cancel()
			<-p.slots
			p.shutdown(pluginapi.Invalid("duplicate IPC request id"))
			return
		}
		p.running[f.ID] = cancel
	}
	p.mu.Unlock()
	go func() {
		defer func() { cancel(); p.mu.Lock(); delete(p.running, f.ID); p.mu.Unlock(); <-p.slots }()
		result, err := func() (result any, err error) {
			defer func() {
				if v := recover(); v != nil {
					err = pluginapi.Fail("INTERNAL", "plugin handler panicked")
				}
			}()
			if p.handler == nil {
				return nil, pluginapi.Fail("UNSUPPORTED", "no IPC request handler")
			}
			return p.handler(ctx, f.Method, f.Params)
		}()
		if f.ID != "" {
			p.respond(f.ID, result, err)
		}
	}()
}
func (p *Peer) respond(id string, result any, err error) {
	f := frame{JSONRPC: "2.0", ID: id}
	if err == nil {
		f.Result, err = json.Marshal(result)
	}
	if err != nil {
		f.Result = nil
		var failure *pluginapi.Failure
		if !errors.As(err, &failure) {
			failure = &pluginapi.Failure{Code: "INTERNAL", Message: err.Error()}
		}
		f.Error = &rpcError{Code: -32000, Message: failure.Message, Data: failure}
	}
	p.enqueue(f)
}
