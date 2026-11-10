package main

import (
	"testing"
	"time"
)

func TestSessionRegistry(t *testing.T) {
	var reg sessionRegistry
	ws := &safeWebSocket{}

	reg.register(ws)

	done := make(chan struct{})
	go func() {
		reg.wait()
		close(done)
	}()

	reg.unregister(ws)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wait did not return after unregister")
	}
}
