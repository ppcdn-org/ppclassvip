package realtime

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const sendBuffer = 64

type Client struct {
	RoomID        uint64
	ParticipantID uint64
	Role          string

	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once
}

func NewClient(roomID, participantID uint64, role string, conn *websocket.Conn) *Client {
	return &Client{
		RoomID:        roomID,
		ParticipantID: participantID,
		Role:          role,
		conn:          conn,
		send:          make(chan []byte, sendBuffer),
	}
}

func (c *Client) Enqueue(payload []byte) {
	select {
	case c.send <- payload:
	default:
		// Slow clients drop events instead of blocking the room.
	}
}

func (c *Client) Send(event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	c.Enqueue(payload)
}

func (c *Client) WritePump() {
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

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		_ = c.conn.Close()
	})
}

// CloseWithReason sends a websocket close frame with the given reason so the
// browser can distinguish a deliberate room shutdown from a transient network
// drop and skip reconnecting.
func (c *Client) CloseWithReason(reason string) {
	c.closeOnce.Do(func() {
		_ = c.conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason),
			time.Now().Add(2*time.Second),
		)
		_ = c.conn.Close()
	})
}

type Hub struct {
	mu    sync.RWMutex
	rooms map[uint64]map[uint64]*Client
}

func NewHub() *Hub {
	return &Hub{rooms: make(map[uint64]map[uint64]*Client)}
}

func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room := h.rooms[client.RoomID]
	if room == nil {
		room = make(map[uint64]*Client)
		h.rooms[client.RoomID] = room
	}
	if old := room[client.ParticipantID]; old != nil && old != client {
		old.Close()
	}
	room[client.ParticipantID] = client
}

func (h *Hub) Unregister(client *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	room := h.rooms[client.RoomID]
	if room == nil {
		return false
	}
	removed := false
	if current := room[client.ParticipantID]; current == client {
		delete(room, client.ParticipantID)
		removed = true
	}
	if len(room) == 0 {
		delete(h.rooms, client.RoomID)
	}
	return removed
}

func (h *Hub) Broadcast(roomID uint64, event any) {
	h.BroadcastExcept(roomID, 0, event)
}

func (h *Hub) BroadcastExcept(roomID, exceptParticipantID uint64, event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.rooms[roomID]))
	for participantID, client := range h.rooms[roomID] {
		if participantID == exceptParticipantID {
			continue
		}
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		client.Enqueue(payload)
	}
}

func (h *Hub) SendTo(roomID, participantID uint64, event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.RLock()
	client := h.rooms[roomID][participantID]
	h.mu.RUnlock()
	if client != nil {
		client.Enqueue(payload)
	}
}

func (h *Hub) Count(roomID uint64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[roomID])
}

func (h *Hub) CloseRoom(roomID uint64) {
	h.CloseRoomWithReason(roomID, "")
}

func (h *Hub) CloseRoomWithReason(roomID uint64, reason string) {
	h.mu.Lock()
	room := h.rooms[roomID]
	delete(h.rooms, roomID)
	h.mu.Unlock()

	for _, client := range room {
		if reason == "" {
			client.Close()
			continue
		}
		client.CloseWithReason(reason)
	}
}
