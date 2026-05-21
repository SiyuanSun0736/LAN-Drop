package ipc

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/landrop/landrop/backend/internal/events"
)

type Hub struct {
	mu       sync.RWMutex
	clients  map[*client]struct{}
	upgrader websocket.Upgrader
}

type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
	}
}

func (h *Hub) ServeWS(writer http.ResponseWriter, request *http.Request) {
	conn, err := h.upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}

	client := &client{conn: conn}
	h.register(client)
	_ = client.writeJSON(events.New("agent.connected", map[string]string{"message": "websocket ready"}))

	go func() {
		defer h.unregister(client)

		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()
}

func (h *Hub) Broadcast(event events.Event) {
	h.mu.RLock()
	clients := make([]*client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		if err := client.writeJSON(event); err != nil {
			h.unregister(client)
		}
	}
}

func (h *Hub) register(client *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = struct{}{}
}

func (h *Hub) unregister(client *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client]; !ok {
		return
	}

	delete(h.clients, client)
	_ = client.conn.Close()
}

func (c *client) writeJSON(payload any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.conn.WriteJSON(payload)
}