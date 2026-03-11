package manager

import (
	"net/http"
	"strings"

	webassets "workspace-protocol/shelleymanager/web"
)

func (m *Manager) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	webassets.ServeIndex(w, r)
}

func (m *Manager) handleWSLanguage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	webassets.ServeIndex(w, r)
}

func (m *Manager) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/app/"))
	if len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	namespace, workspace := parts[0], parts[1]
	if _, ok := m.getWorkspace(namespace, workspace); !ok {
		http.NotFound(w, r)
		return
	}
	webassets.ServeIndex(w, r)
}

func (m *Manager) handleUIAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	webassets.ServeAsset(w, r)
}

func (m *Manager) handleShelleyUIRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/shelley/"))
	if len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	namespace, workspace := parts[0], parts[1]
	ws, ok := m.getWorkspace(namespace, workspace)
	if !ok {
		http.NotFound(w, r)
		return
	}
	target := strings.TrimRight(ws.Runtime.APIBase.String(), "/")
	if len(parts) == 3 {
		target += "/c/" + parts[2]
	}
	http.Redirect(w, r, target, http.StatusFound)
}
