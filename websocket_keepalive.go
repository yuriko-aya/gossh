package main

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsPingInterval = 30 * time.Second
	wsPongWait     = 90 * time.Second
	wsWriteWait    = 10 * time.Second
)

// safeWebSocket wraps a WebSocket connection with a mutex because gorilla/websocket
// permits only one concurrent writer (data and control frames share the writer lock).
type safeWebSocket struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func newSafeWebSocket(conn *websocket.Conn) *safeWebSocket {
	return &safeWebSocket{conn: conn}
}

func (s *safeWebSocket) WriteMessage(messageType int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(messageType, data)
}

func (s *safeWebSocket) writePing(deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return s.conn.WriteControl(websocket.PingMessage, nil, deadline)
}

func (s *safeWebSocket) Close() error {
	return s.conn.Close()
}

func (s *safeWebSocket) ReadMessage() (messageType int, p []byte, err error) {
	return s.conn.ReadMessage()
}

func (s *safeWebSocket) SetPongHandler(h func(appData string) error) {
	s.conn.SetPongHandler(h)
}

func (s *safeWebSocket) SetReadDeadline(t time.Time) error {
	return s.conn.SetReadDeadline(t)
}

// startWebSocketKeepalive sends native WebSocket ping frames until stop is closed.
// The browser responds with pong frames automatically at the protocol level.
func startWebSocketKeepalive(ws *safeWebSocket, stop <-chan struct{}, onFailed func(reason string)) {
	if err := ws.SetReadDeadline(time.Now().Add(wsPongWait)); err != nil {
		log.Printf("Failed to set websocket read deadline: %v", err)
	}

	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	ticker := time.NewTicker(wsPingInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				deadline := time.Now().Add(wsWriteWait)
				if err := ws.writePing(deadline); err != nil {
					log.Printf("WebSocket ping failed: %v", err)
					onFailed("websocket ping failed")
					return
				}
			}
		}
	}()
}
