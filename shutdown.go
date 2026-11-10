package main

import (
	"log"
	"sync"
)

type sessionRegistry struct {
	mu       sync.Mutex
	sessions map[*safeWebSocket]struct{}
	wg       sync.WaitGroup
}

var activeSessions sessionRegistry

func (r *sessionRegistry) register(ws *safeWebSocket) {
	r.wg.Add(1)
	r.mu.Lock()
	if r.sessions == nil {
		r.sessions = make(map[*safeWebSocket]struct{})
	}
	r.sessions[ws] = struct{}{}
	r.mu.Unlock()
}

func (r *sessionRegistry) unregister(ws *safeWebSocket) {
	r.mu.Lock()
	_, ok := r.sessions[ws]
	if ok {
		delete(r.sessions, ws)
	}
	r.mu.Unlock()
	if ok {
		r.wg.Done()
	}
}

func (r *sessionRegistry) closeAll() {
	r.mu.Lock()
	sessions := make([]*safeWebSocket, 0, len(r.sessions))
	for ws := range r.sessions {
		sessions = append(sessions, ws)
	}
	r.mu.Unlock()

	for _, ws := range sessions {
		if err := ws.Close(); err != nil {
			log.Printf("Error closing websocket during shutdown: %v", err)
		}
	}
}

func (r *sessionRegistry) wait() {
	r.wg.Wait()
}
