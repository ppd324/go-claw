package server

import (
	"encoding/json"

	"github.com/gorilla/websocket"
)

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Client represents a connected WebSocket client
type Client struct {
	ID             string
	Conn           *websocket.Conn
	Send           chan []byte
	AgentID        uint
	UserID         uint
	Platform       string
	PlatformChatID string
}

// ClientManager manages connected clients
type ClientManager struct {
	clients    map[string]*Client
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

// NewClientManager creates a new client manager
func NewClientManager() *ClientManager {
	return &ClientManager{
		clients:    make(map[string]*Client),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client, 100),
		unregister: make(chan *Client, 100),
	}
}

// Start starts the client manager
func (m *ClientManager) Start() {
	go func() {
		for {
			select {
			case client := <-m.register:
				m.clients[client.ID] = client
			case client := <-m.unregister:
				if _, ok := m.clients[client.ID]; ok {
					close(client.Send)
					delete(m.clients, client.ID)
				}
			case message := <-m.broadcast:
				for _, client := range m.clients {
					select {
					case client.Send <- message:
					default:
						close(client.Send)
						delete(m.clients, client.ID)
					}
				}
			}
		}
	}()
}

// GetClient returns a client by ID
func (m *ClientManager) GetClient(id string) (*Client, bool) {
	client, ok := m.clients[id]
	return client, ok
}

// GetClientsByAgent returns all clients for a specific agent
func (m *ClientManager) GetClientsByAgent(agentID uint) []*Client {
	var result []*Client
	for _, client := range m.clients {
		if client.AgentID == agentID {
			result = append(result, client)
		}
	}
	return result
}

// GetClientsByUser returns all clients for a specific user
func (m *ClientManager) GetClientsByUser(userID uint) []*Client {
	var result []*Client
	for _, client := range m.clients {
		if client.UserID == userID {
			result = append(result, client)
		}
	}
	return result
}

// SendToClient sends a message to a specific client
func (m *ClientManager) SendToClient(clientID string, message []byte) {
	if client, ok := m.clients[clientID]; ok {
		select {
		case client.Send <- message:
		default:
		}
	}
}

// BroadcastToAgent broadcasts a message to all clients of an agent
func (m *ClientManager) BroadcastToAgent(agentID uint, message []byte) {
	for _, client := range m.clients {
		if client.AgentID == agentID {
			select {
			case client.Send <- message:
			default:
			}
		}
	}
}

// BroadcastToUser broadcasts a message to all clients of a user
func (m *ClientManager) BroadcastToUser(userID uint, message []byte) {
	for _, client := range m.clients {
		if client.UserID == userID {
			select {
			case client.Send <- message:
			default:
			}
		}
	}
}

// ClientCount returns the number of connected clients
func (m *ClientManager) ClientCount() int {
	return len(m.clients)
}
