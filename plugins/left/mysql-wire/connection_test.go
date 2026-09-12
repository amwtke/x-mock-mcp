package main

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestConnectionDetectsEOFWhileHandlerWaits(t *testing.T) {
	server, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connection := newObservedConn(server, cancel)
	defer connection.Close()
	write := make(chan error, 1)
	go func() { _, err := client.Write([]byte{1, 0, 0, 0, 3}); write <- err }()
	frame := make([]byte, 5)
	if _, err := io.ReadFull(connection, frame); err != nil {
		t.Fatal(err)
	}
	if err := <-write; err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("EOF did not cancel waiting request")
	}
}
