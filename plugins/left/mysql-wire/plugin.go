package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/server"
	"github.com/go-mysql-org/go-mysql/stmt"
	"net"
	"strconv"
	"sync"
	"time"
	"xmock.local/x-mock-mcp/contracts/mysqlv1"
	"xmock.local/x-mock-mcp/pluginapi"
)

type config struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type plugin struct {
	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]bool
	wg       sync.WaitGroup
}

func (*plugin) Describe(context.Context) (pluginapi.Descriptor, error) {
	return pluginapi.Descriptor{Ref: pluginapi.Reference{ID: "mysql-wire", Version: "0.1.0", Role: pluginapi.Left}, APIMajor: 1, Contracts: []pluginapi.Contract{{ID: "mysql.operation", Version: 1, Capabilities: []string{"query", "prepare", "bigint", "varchar", "writes", "transactions"}}}, ConfigSchema: mysqlv1.SchemaFor[config](), Features: []string{}}, nil
}
func (*plugin) ValidateConfig(_ context.Context, raw json.RawMessage) error {
	c := config{Port: 3306}
	if err := pluginapi.Decode(raw, &c); err != nil {
		return err
	}
	if c.Port < 0 || c.Port > 65535 || c.Username == "" || c.Password == "" {
		return pluginapi.Invalid("valid port and dedicated test credentials required")
	}
	if c.Host != "" && c.Host != "localhost" {
		ip := net.ParseIP(c.Host)
		if ip == nil || !ip.IsLoopback() {
			return pluginapi.Invalid("P0 MySQL listener must use loopback")
		}
	}
	return nil
}
func (p *plugin) Start(ctx context.Context, spec pluginapi.InstanceSpec, dispatch pluginapi.Dispatch) (pluginapi.Ready, error) {
	if err := p.ValidateConfig(ctx, spec.Config); err != nil {
		return pluginapi.Ready{}, err
	}
	c := config{Port: 3306}
	pluginapi.Decode(spec.Config, &c)
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener != nil {
		return pluginapi.Ready{}, pluginapi.Fail("STATE_CONFLICT", "left already started")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return pluginapi.Ready{}, err
	}
	p.listener = listener
	p.conns = map[net.Conn]bool{}
	conf := server.NewServer("8.0.0-xmock", 46, mysql.AUTH_NATIVE_PASSWORD, nil, nil)
	if err = conf.UnsetCapability(mysql.CLIENT_MULTI_RESULTS | mysql.CLIENT_PS_MULTI_RESULTS); err != nil {
		listener.Close()
		p.listener = nil
		return pluginapi.Ready{}, err
	}
	auth := server.NewInMemoryAuthenticationHandler(mysql.AUTH_NATIVE_PASSWORD)
	if err = auth.AddUser(c.Username, c.Password); err != nil {
		listener.Close()
		p.listener = nil
		return pluginapi.Ready{}, err
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			if p.listener != listener || len(p.conns) >= 32 {
				p.mu.Unlock()
				conn.Close()
				continue
			}
			p.conns[conn] = true
			p.wg.Add(1)
			p.mu.Unlock()
			go p.serve(conn, conf, auth, dispatch)
		}
	}()
	return pluginapi.Ready{InstanceID: spec.InstanceID, Endpoints: map[string]string{"mysql": listener.Addr().String()}}, nil
}

type authentication struct {
	server.AuthenticationHandler
	h    *handler
	wire *observedConn
}

func (a *authentication) OnAuthSuccess(c *server.Conn) error {
	a.h.conn = c
	raw, err := a.h.call("connection.open", map[string]bool{"found_rows": c.HasCapability(mysql.CLIENT_FOUND_ROWS)})
	if err != nil {
		return err
	}
	if _, err = a.h.result(raw, false); err != nil {
		return err
	}
	a.wire.commandPhase.Store(true)
	return nil
}
func (p *plugin) serve(socket net.Conn, conf *server.Server, auth server.AuthenticationHandler, dispatch pluginapi.Dispatch) {
	defer p.wg.Done()
	defer func() { p.mu.Lock(); delete(p.conns, socket); p.mu.Unlock() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket.SetDeadline(time.Now().Add(5 * time.Second))
	wire := newObservedConn(socket, cancel)
	defer wire.Close()
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return
	}
	h := &handler{ctx: ctx, id: hex.EncodeToString(idBytes), dispatch: dispatch, statements: map[*stmt.PreparedStmt]mysqlv1.Metadata{}}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		dispatch(cleanup, pluginapi.Request{ConnectionID: h.id, Operation: "connection.close"})
	}()
	conn, err := conf.NewCustomizedConn(wire, &authentication{AuthenticationHandler: auth, h: h, wire: wire}, h)
	if err != nil {
		return
	}
	defer func() {
		if conn.Conn != nil {
			conn.Close()
		}
	}()
	socket.SetDeadline(time.Time{})
	for ctx.Err() == nil {
		if err = conn.HandleCommand(); err != nil {
			return
		}
	}
}
func (p *plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	listener := p.listener
	p.listener = nil
	if listener != nil {
		listener.Close()
	}
	for c := range p.conns {
		c.Close()
	}
	p.mu.Unlock()
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop MySQL left: %w", ctx.Err())
	}
}
