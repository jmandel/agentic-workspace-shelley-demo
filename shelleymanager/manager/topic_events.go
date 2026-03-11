package manager

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	workspaceHeaderSubject     = "X-Workspace-Subject"
	workspaceHeaderDisplayName = "X-Workspace-Display-Name"
)

func (m *Manager) handleTopicEvents(w http.ResponseWriter, r *http.Request, ws *Workspace, topic string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := validateName(topic); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		m.logger.Error("topic websocket accept failed", "workspace", ws.Name, "topic", topic, "error", err)
		return
	}
	defer clientConn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	principal, err := m.authenticateWebSocket(ctx, clientConn, r)
	if err != nil {
		return
	}

	upstreamHeaders := http.Header{}
	upstreamHeaders.Set(workspaceHeaderSubject, principal.Subject)
	upstreamHeaders.Set(workspaceHeaderDisplayName, principal.DisplayName)

	upstreamURL := runtimeTopicEventsURL(ws, topic)
	upstreamConn, _, err := websocket.Dial(ctx, upstreamURL, &websocket.DialOptions{
		HTTPHeader: upstreamHeaders,
	})
	if err != nil {
		m.logger.Error("topic websocket upstream dial failed", "workspace", ws.Name, "topic", topic, "url", upstreamURL, "error", err)
		_ = wsjson.Write(ctx, clientConn, map[string]any{
			"type": "error",
			"data": "runtime unavailable",
		})
		_ = clientConn.Close(websocket.StatusInternalError, "runtime unavailable")
		return
	}
	defer upstreamConn.CloseNow()

	errCh := make(chan error, 2)
	go relayWebSocketFrames(ctx, upstreamConn, clientConn, errCh)
	go relayWebSocketFrames(ctx, clientConn, upstreamConn, errCh)

	if err := <-errCh; err != nil && ctx.Err() == nil {
		cancel()
	}
}

func runtimeTopicEventsURL(ws *Workspace, topic string) string {
	target := *ws.Runtime.APIBase
	switch strings.ToLower(target.Scheme) {
	case "https":
		target.Scheme = "wss"
	case "http":
		target.Scheme = "ws"
	}
	target.Path = cleanProxyPath(path.Join(target.Path, "ws", "topics", url.PathEscape(topic), "events"))
	target.RawPath = target.Path
	return target.String()
}

func relayWebSocketFrames(ctx context.Context, dst, src *websocket.Conn, errCh chan<- error) {
	for {
		msgType, data, err := src.Read(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if err := dst.Write(ctx, msgType, data); err != nil {
			errCh <- err
			return
		}
	}
}
