package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// eventClient represents a single WebSocket subscriber.
type eventClient struct {
	send chan []byte
}

// EventHub manages lifecycle event broadcasting to WebSocket clients.
type EventHub struct {
	counter atomic.Int64

	mu      sync.Mutex
	clients map[string]map[*eventClient]struct{} // namespace → clients
	closed  bool
}

func newEventHub() *EventHub {
	return &EventHub{
		clients: make(map[string]map[*eventClient]struct{}),
	}
}

func (h *EventHub) register(namespace string) *eventClient {
	c := &eventClient{send: make(chan []byte, 64)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(c.send)
		return c
	}
	if h.clients[namespace] == nil {
		h.clients[namespace] = make(map[*eventClient]struct{})
	}
	h.clients[namespace][c] = struct{}{}
	return c
}

func (h *EventHub) unregister(namespace string, c *eventClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[namespace]; ok {
		if _, exists := set[c]; exists {
			delete(set, c)
			close(c.send)
		}
		if len(set) == 0 {
			delete(h.clients, namespace)
		}
	}
}

func (h *EventHub) nextID() int64 {
	return h.counter.Add(1)
}

// emit broadcasts a lifecycle event to all clients in the given namespace.
func (h *EventHub) emit(namespace, eventType string, fields map[string]any) {
	msg := map[string]any{
		"type":      eventType,
		"eventId":   fmt.Sprintf("me_%d", h.nextID()),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range fields {
		msg[k] = v
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients[namespace] {
		select {
		case c.send <- data:
		default:
			// Client too slow; drop the message.
		}
	}
}

// closeAll shuts down all event clients.
func (h *EventHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ns, set := range h.clients {
		for c := range set {
			close(c.send)
		}
		delete(h.clients, ns)
	}
}

// handleEvents is the WebSocket handler for /apis/v1/namespaces/{namespace}/events.
func (m *Manager) handleEvents(w http.ResponseWriter, r *http.Request, namespace string) {
	if err := validateName(namespace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if namespace != m.defaultNamespace {
		http.NotFound(w, r)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		m.logger.Error("events websocket accept failed", "error", err)
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	if _, err := m.authenticateWebSocket(ctx, conn, r); err != nil {
		return
	}
	ctx = conn.CloseRead(ctx)

	client := m.events.register(namespace)
	defer m.events.unregister(namespace, client)

	// Send connected message.
	connected, _ := json.Marshal(map[string]any{
		"type":            "connected",
		"protocolVersion": "workspace-manager-v1",
		"namespace":       namespace,
		"replay":          true,
	})
	if err := conn.Write(ctx, websocket.MessageText, connected); err != nil {
		return
	}

	// Replay burst: current workspaces and their topics.
	m.sendReplayBurst(ctx, conn, namespace)

	// Live tail: forward events from hub to WebSocket.
	for {
		select {
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) sendReplayBurst(ctx context.Context, conn *websocket.Conn, namespace string) {
	workspaces := m.listWorkspaces(namespace)
	for _, ws := range workspaces {
		topics := m.runtimeTopics(ctx, ws)

		topicRefs := make([]map[string]string, 0, len(topics))
		for _, t := range topics {
			topicRefs = append(topicRefs, map[string]string{"name": t.Name})
		}

		event, _ := json.Marshal(map[string]any{
			"type":      "workspace_created",
			"eventId":   fmt.Sprintf("me_%d", m.events.nextID()),
			"timestamp": ws.CreatedAt.Format(time.RFC3339),
			"replay":    true,
			"workspace": map[string]any{
				"name":      ws.Name,
				"status":    "running",
				"template":  ws.Template,
				"createdAt": ws.CreatedAt.Format(time.RFC3339),
				"topics":    topicRefs,
			},
		})
		if conn.Write(ctx, websocket.MessageText, event) != nil {
			return
		}
	}
}
