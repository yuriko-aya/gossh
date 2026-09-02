package main

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsPingInterval = 30 * time.Second
	wsPongWait     = 90 * time.Second
	wsWriteWait    = 10 * time.Second
)

// startWebSocketKeepalive sends native WebSocket ping frames until stop is closed.
// The browser responds with pong frames automatically at the protocol level.
func startWebSocketKeepalive(conn *websocket.Conn, stop <-chan struct{}, onFailed func(reason string)) {
	if err := conn.SetReadDeadline(time.Now().Add(wsPongWait)); err != nil {
		log.Printf("Failed to set websocket read deadline: %v", err)
	}

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
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
				if err := conn.SetWriteDeadline(deadline); err != nil {
					onFailed("websocket ping failed")
					return
				}
				if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
					onFailed("websocket ping failed")
					return
				}
			}
		}
	}()
}
