// Package realtime pushes milestone events to connected players over WebSocket.
package realtime

import (
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Hub tracks live connections keyed by athlete_id and fans messages out to them.
type Hub struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[uuid.UUID]map[*Client]struct{})}
}

// Client is one player's WebSocket connection with a buffered send queue and its own writer.
type Client struct {
	hub       *Hub
	athleteID uuid.UUID
	conn      *websocket.Conn
	send      chan []byte
}

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.clients[c.athleteID]
	if set == nil {
		set = make(map[*Client]struct{})
		h.clients[c.athleteID] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.clients[c.athleteID]; set != nil {
		if _, ok := set[c]; ok {
			delete(set, c)
			close(c.send)
		}
		if len(set) == 0 {
			delete(h.clients, c.athleteID)
		}
	}
}

// Push delivers msg to every live connection for the athlete. Slow clients are dropped
// (bounded queue) rather than blocking the fan-out.
func (h *Hub) Push(athleteID uuid.UUID, msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[athleteID] {
		select {
		case c.send <- msg:
		default: // queue full: drop to protect the hub
		}
	}
}
