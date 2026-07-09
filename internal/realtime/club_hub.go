package realtime

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ClubHub broadcasts every milestone event to all connected club-console viewers. Unlike
// Hub, it is not scoped to one athlete_id — the club needs visibility across its whole
// roster (e.g. "bonus fired for player X"), whereas a player must only ever see their own.
type ClubHub struct {
	mu      sync.RWMutex
	clients map[*ClubClient]struct{}
}

func NewClubHub() *ClubHub {
	return &ClubHub{clients: make(map[*ClubClient]struct{})}
}

type ClubClient struct {
	conn *websocket.Conn
	send chan []byte
}

func (h *ClubHub) add(c *ClubClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *ClubHub) remove(c *ClubClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

// Broadcast delivers msg to every connected club viewer. Slow clients are dropped (bounded
// queue) rather than blocking the fan-out.
func (h *ClubHub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
		}
	}
}

// ClubStreamHandler upgrades /api/v1/club/stream — the club console's live feed across
// its whole roster (no per-athlete scoping; see package doc in internal/club for the
// deferred-auth note).
type ClubStreamHandler struct {
	hub      *ClubHub
	upgrader websocket.Upgrader
}

func NewClubStreamHandler(hub *ClubHub) *ClubStreamHandler {
	return &ClubStreamHandler{
		hub:      hub,
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
}

func (h *ClubStreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &ClubClient{conn: conn, send: make(chan []byte, 32)}
	h.hub.add(c)

	go c.writePump()
	c.readPump(h.hub)
}

func (c *ClubClient) readPump(hub *ClubHub) {
	defer func() {
		hub.remove(c)
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

func (c *ClubClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
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
