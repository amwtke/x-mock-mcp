package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/go-mysql-org/go-mysql/mysql"
	"io"
	"net"
	"sync"
	"sync/atomic"
)

// observedConn has exactly one reader of the physical socket. The protocol
// library consumes queued complete frames, so EOF cancels a query even while
// that library is synchronously waiting for a coding agent response.
type observedConn struct {
	net.Conn
	frames         chan []byte
	done           chan struct{}
	once           sync.Once
	cancel         context.CancelFunc
	current        []byte
	mu             sync.Mutex
	err            error
	commandPhase   atomic.Bool
	wroteHandshake bool
}

func newObservedConn(conn net.Conn, cancel context.CancelFunc) *observedConn {
	c := &observedConn{Conn: conn, frames: make(chan []byte, 4), done: make(chan struct{}), cancel: cancel}
	go c.pump()
	return c
}
func (c *observedConn) end(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
		c.cancel()
		c.Conn.Close()
	})
}
func (c *observedConn) Close() error { c.end(io.EOF); return nil }

// go-mysql v1.16.0 does not expose CLIENT_FOUND_ROWS as a configurable
// server capability. Advertise that supported semantic in HandshakeV10;
// its parser preserves the client's negotiated bit for connection.open.
func (c *observedConn) Write(data []byte) (int, error) {
	if !c.wroteHandshake {
		c.wroteHandshake = true
		if len(data) < 5 || data[3] != 0 || data[4] != 10 {
			return 0, fmt.Errorf("invalid initial handshake")
		}
		end := bytes.IndexByte(data[5:], 0)
		if end < 0 {
			return 0, fmt.Errorf("invalid handshake version")
		}
		capOffset := 5 + end + 1 + 4 + 8 + 1
		if len(data) < capOffset+5 {
			return 0, fmt.Errorf("short initial handshake")
		}
		data = bytes.Clone(data)
		data[capOffset] |= byte(mysql.CLIENT_FOUND_ROWS)
		data[capOffset+3] |= byte(mysql.SERVER_STATUS_AUTOCOMMIT)
	}
	return c.Conn.Write(data)
}
func (c *observedConn) Read(p []byte) (int, error) {
	if len(c.current) > 0 {
		n := copy(p, c.current)
		c.current = c.current[n:]
		return n, nil
	}
	select {
	case frame := <-c.frames:
		c.current = frame
		return c.Read(p)
	case <-c.done:
		c.mu.Lock()
		err := c.err
		c.mu.Unlock()
		return 0, err
	}
}
func (c *observedConn) pump() {
	for {
		header := make([]byte, 4)
		if _, err := io.ReadFull(c.Conn, header); err != nil {
			c.end(err)
			return
		}
		size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
		if size == 0 || size > 1<<20 {
			c.end(fmt.Errorf("MySQL packet exceeds supported size or is empty"))
			return
		}
		frame := make([]byte, 4+size)
		copy(frame, header)
		if _, err := io.ReadFull(c.Conn, frame[4:]); err != nil {
			c.end(err)
			return
		}
		if c.commandPhase.Load() {
			switch frame[4] {
			case mysql.COM_QUERY, mysql.COM_QUIT, mysql.COM_PING, mysql.COM_INIT_DB, mysql.COM_STMT_PREPARE, mysql.COM_STMT_EXECUTE, mysql.COM_STMT_CLOSE, mysql.COM_STMT_RESET:
			default:
				c.end(fmt.Errorf("unsupported MySQL command %d", frame[4]))
				return
			}
		}
		select {
		case c.frames <- frame:
		case <-c.done:
			return
		default:
			c.end(fmt.Errorf("MySQL frame queue exhausted"))
			return
		}
	}
}
