package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"xmock.local/x-mock-mcp/pluginapi"
	"xmock.local/x-mock-mcp/pluginapi/host"
)

var pluginID = "echo-left"

type config struct {
	Listen string `json:"listen"`
}
type plugin struct {
	mu          sync.Mutex
	listener    net.Listener
	connections map[net.Conn]context.CancelFunc
	wg          sync.WaitGroup
}

func (p *plugin) Describe(context.Context) (pluginapi.Descriptor, error) {
	return pluginapi.Descriptor{Ref: pluginapi.Reference{ID: pluginID, Version: "0.1.0", Role: pluginapi.Left}, APIMajor: 1, Contracts: []pluginapi.Contract{{ID: "echo", Version: 1, Capabilities: []string{"echo"}}}, ConfigSchema: json.RawMessage(`{"type":"object","properties":{"listen":{"type":"string"}},"additionalProperties":false}`)}, nil
}
func (p *plugin) ValidateConfig(ctx context.Context, raw json.RawMessage) error {
	var c config
	return pluginapi.Decode(raw, &c)
}
func (p *plugin) Start(ctx context.Context, s pluginapi.InstanceSpec, dispatch pluginapi.Dispatch) (pluginapi.Ready, error) {
	var c config
	if err := pluginapi.Decode(s.Config, &c); err != nil {
		return pluginapi.Ready{}, err
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return pluginapi.Ready{}, pluginapi.Fail("PORT_IN_USE", err.Error())
	}
	p.mu.Lock()
	p.listener = listener
	p.connections = map[net.Conn]context.CancelFunc{}
	p.wg.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connectionCtx, cancel := context.WithCancel(context.Background())
			p.mu.Lock()
			if p.listener == nil {
				p.mu.Unlock()
				cancel()
				conn.Close()
				return
			}
			p.connections[conn] = cancel
			p.wg.Add(1)
			p.mu.Unlock()
			go func() {
				defer p.wg.Done()
				defer func() { cancel(); conn.Close(); p.mu.Lock(); delete(p.connections, conn); p.mu.Unlock() }()
				scanner := bufio.NewScanner(conn)
				scanner.Buffer(make([]byte, 1024), 65536)
				for scanner.Scan() {
					result, err := dispatch(connectionCtx, pluginapi.Request{ConnectionID: conn.RemoteAddr().String(), Operation: "echo", Payload: append(json.RawMessage(nil), scanner.Bytes()...)})
					if err != nil {
						result, _ = json.Marshal(map[string]string{"error": err.Error()})
					}
					if _, err = conn.Write(append(result, '\n')); err != nil {
						return
					}
				}
			}()
		}
	}()
	return pluginapi.Ready{InstanceID: s.InstanceID, Endpoints: map[string]string{"tcp": listener.Addr().String()}}, nil
}
func (p *plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.listener != nil {
		p.listener.Close()
		p.listener = nil
	}
	for c, cancel := range p.connections {
		cancel()
		c.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
	return nil
}
func main() {
	if err := host.ServeLeft(&plugin{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
