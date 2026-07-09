package realtime

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jetlum/playerboard/internal/auth"
	"github.com/jetlum/playerboard/internal/platform/httpx"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 50 * time.Second
)

// Handler upgrades /me/stream to a WebSocket bound to the authenticated athlete.
type Handler struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

func NewHandler(hub *Hub) *Handler {
	return &Handler{
		hub: hub,
		upgrader: websocket.Upgrader{
			// Same-origin isn't meaningful for a token-authed API; the JWT is the gate.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	athleteID, ok := auth.AthleteID(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrader already wrote the error
	}
	client := &Client{hub: h.hub, athleteID: athleteID, conn: conn, send: make(chan []byte, 32)}
	h.hub.add(client)

	go client.writePump()
	client.readPump() // blocks until the socket closes
}

// readPump drains inbound frames (we don't expect any) and detects disconnects.
func (c *Client) readPump() {
	defer func() {
		c.hub.remove(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump serializes all writes for this connection and keeps it alive with pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok { // hub closed the channel
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
