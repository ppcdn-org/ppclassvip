package realtime

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ControlClient struct {
	SessionID string
	Role      string

	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once
}

func NewControlClient(sessionID, role string, conn *websocket.Conn) *ControlClient {
	return &ControlClient{
		SessionID: sessionID,
		Role:      role,
		conn:      conn,
		send:      make(chan []byte, 128),
	}
}

func (c *ControlClient) Enqueue(payload []byte) {
	select {
	case c.send <- payload:
	default:
		// Drop when a participant cannot keep up; input events are best-effort.
	}
}

func (c *ControlClient) WritePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer c.Close()

	for {
		select {
		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *ControlClient) Close() {
	c.closeOnce.Do(func() {
		_ = c.conn.Close()
	})
}

type ControlHub struct {
	mu       sync.Mutex
	sessions map[string]map[string]*ControlClient
}

func NewControlHub() *ControlHub {
	return &ControlHub{sessions: make(map[string]map[string]*ControlClient)}
}

func (h *ControlHub) Register(sessionID, role string, conn *websocket.Conn) *ControlClient {
	client := NewControlClient(sessionID, role, conn)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions[sessionID] == nil {
		h.sessions[sessionID] = make(map[string]*ControlClient)
	}
	if old := h.sessions[sessionID][role]; old != nil {
		old.Close()
	}
	h.sessions[sessionID][role] = client
	return client
}

func (h *ControlHub) Unregister(client *ControlClient) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.sessions[client.SessionID]
	if room == nil {
		return false
	}
	if room[client.Role] != client {
		return false
	}
	delete(room, client.Role)
	if len(room) == 0 {
		delete(h.sessions, client.SessionID)
	}
	return true
}

func (h *ControlHub) Forward(sessionID, fromRole string, payload []byte) {
	targetRole := "agent"
	if fromRole == "agent" {
		targetRole = "controller"
	}
	h.mu.Lock()
	client := h.sessions[sessionID][targetRole]
	h.mu.Unlock()
	if client != nil {
		client.Enqueue(payload)
	}
}

func (h *ControlHub) HasAgent(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[sessionID]["agent"] != nil
}

func (h *ControlHub) CloseSession(sessionID string) {
	h.mu.Lock()
	clients := h.sessions[sessionID]
	delete(h.sessions, sessionID)
	h.mu.Unlock()
	for _, client := range clients {
		client.Close()
	}
}
