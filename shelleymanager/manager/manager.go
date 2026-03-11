package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

type Config struct {
	DefaultNamespace string
	Launcher         Launcher
	LocalTools       []LocalTool
	StateRoot        string
	ShelleyUIMode    string
	Logger           *slog.Logger
}

type Manager struct {
	defaultNamespace string
	launcher         Launcher
	localTools       []LocalTool
	stateRoot        string
	shelleyUIMode    string
	logger           *slog.Logger
	tokenValidator   tokenValidator
	internalBaseURL  string

	mu         sync.RWMutex
	workspaces map[workspaceKey]*Workspace

	events *EventHub
}

type workspaceKey struct {
	namespace string
	name      string
}

type Workspace struct {
	Namespace  string
	Name       string
	StateDir   string
	CreatedAt  time.Time
	Template   string
	LocalTools []LocalTool
	Runtime    Runtime
}

type Runtime struct {
	Name    string
	APIBase *url.URL
	Mode    string
	Stop    func(context.Context) error
	Health  func(context.Context) error
}

type Launcher interface {
	Launch(context.Context, LaunchSpec) (*Runtime, error)
	WorkspacePaths(namespace, name string) (LaunchSpec, error)
	Name() string
}

type LaunchSpec struct {
	Namespace    string
	Name         string
	StateDir     string
	WorkspaceDir string
	DBPath       string
	LocalTools   []LocalTool
	ManagerURL   string
	ManagerToken string
}

type workspaceCreateRequest struct {
	Name     string                   `json:"name"`
	Template string                   `json:"template,omitempty"`
	Topics   json.RawMessage          `json:"topics,omitempty"`
	Runtime  *workspaceRuntimeRequest `json:"runtime,omitempty"`
}

type workspacePatchRequest struct {
	Runtime *workspaceRuntimeRequest `json:"runtime,omitempty"`
}

type workspaceRuntimeRequest struct {
	LocalTools []string `json:"localTools,omitempty"`
}

type workspaceSummary struct {
	ID        string `json:"id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	API       string `json:"api,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type workspaceDetail struct {
	workspaceSummary
	Topics  []workspaceTopicRef   `json:"topics,omitempty"`
	Runtime *workspaceRuntimeInfo `json:"runtime,omitempty"`
}

type workspaceTopicRef struct {
	Name    string `json:"name"`
	Events  string `json:"events,omitempty"`
	Shelley string `json:"shelley,omitempty"`
}

type workspaceRuntimeInfo struct {
	LocalTools []localToolInfo `json:"localTools,omitempty"`
}

type runtimeTopicInfo struct {
	Name string `json:"name"`
}

func New(cfg Config) (*Manager, error) {
	if cfg.Launcher == nil {
		return nil, errors.New("launcher required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	namespace := strings.TrimSpace(cfg.DefaultNamespace)
	if namespace == "" {
		namespace = "default"
	}
	shelleyUIMode := normalizeShelleyUIMode(cfg.ShelleyUIMode)
	if shelleyUIMode == "" {
		return nil, errors.New("invalid Shelley UI mode")
	}
	return &Manager{
		defaultNamespace: namespace,
		launcher:         cfg.Launcher,
		localTools:       append([]LocalTool(nil), cfg.LocalTools...),
		stateRoot:        strings.TrimSpace(cfg.StateRoot),
		shelleyUIMode:    shelleyUIMode,
		logger:           logger,
		tokenValidator:   noneJWTTokenValidator{},
		workspaces:       map[workspaceKey]*Workspace{},
		events:           newEventHub(),
	}, nil
}

func (m *Manager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if managerAuthRequired(r) {
		principal, ok, err := m.authenticateRequest(r)
		if err != nil {
			http.Error(w, "invalid authorization token", http.StatusUnauthorized)
			return
		}
		if !ok {
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}
		r = withRequestPrincipal(r, principal)
	}

	switch {
	case r.URL.Path == "/":
		m.handleHome(w, r)
	case r.URL.Path == "/ws-language":
		m.handleWSLanguage(w, r)
	case strings.HasPrefix(r.URL.Path, "/app/"):
		m.handleApp(w, r)
	case strings.HasPrefix(r.URL.Path, "/assets/"):
		m.handleUIAsset(w, r)
	case strings.HasPrefix(r.URL.Path, "/shelley/"):
		m.handleShelleyUIRedirect(w, r)
	case strings.HasPrefix(r.URL.Path, "/internal/"):
		m.handleInternal(w, r)
	case r.URL.Path == "/health":
		m.handleHealth(w, r)
	case r.URL.Path == "/apis/v1/local-tools":
		m.handleLocalTools(w, r)
	case strings.HasPrefix(r.URL.Path, "/apis/v1/namespaces/"):
		m.handleNamespaced(w, r)
	default:
		http.NotFound(w, r)
	}
}

func managerAuthRequired(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/apis/v1/") && isEventStreamWebSocketRequest(r) {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/apis/v1/")
}

func isEventStreamWebSocketRequest(r *http.Request) bool {
	if !strings.HasSuffix(r.URL.Path, "/events") {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func (m *Manager) Shutdown(ctx context.Context) error {
	m.events.closeAll()

	m.mu.Lock()
	workspaces := make([]*Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		workspaces = append(workspaces, ws)
	}
	m.workspaces = map[workspaceKey]*Workspace{}
	m.mu.Unlock()

	var errs []error
	for _, ws := range workspaces {
		if ws.Runtime.Stop != nil {
			if err := ws.Runtime.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("%s/%s: %w", ws.Namespace, ws.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.mu.RLock()
	count := len(m.workspaces)
	m.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"workspaces":  count,
		"namespace":   m.defaultNamespace,
		"launcher":    m.launcher.Name(),
		"mode":        "shelleymanager",
		"runtimePath": "/ws/*",
		"localTools":  localToolInfos(m.localTools),
	})
}

func (m *Manager) handleLocalTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, localToolInfos(m.localTools))
}

func (m *Manager) handleNamespaced(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/"))
	if len(parts) < 5 || parts[0] != "apis" || parts[1] != "v1" || parts[2] != "namespaces" {
		http.NotFound(w, r)
		return
	}

	namespace := parts[3]
	if err := validateName(namespace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if namespace != m.defaultNamespace {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 5 && parts[4] == "events" {
		m.handleEvents(w, r, namespace)
		return
	}

	if parts[4] != "workspaces" {
		http.NotFound(w, r)
		return
	}

	if len(parts) == 5 {
		switch r.Method {
		case http.MethodGet:
			m.writeSpecList(w, r, namespace)
		case http.MethodPost:
			m.createWorkspace(w, r, namespace)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	name := parts[5]
	if err := validateName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(parts) == 6 {
		switch r.Method {
		case http.MethodGet:
			ws, ok := m.getWorkspace(namespace, name)
			if !ok {
				http.NotFound(w, r)
				return
			}
			m.writeSpecDetail(w, r, ws)
		case http.MethodPatch:
			m.updateWorkspace(w, r, namespace, name)
		case http.MethodDelete:
			m.deleteWorkspace(w, r, namespace, name)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	ws, ok := m.getWorkspace(namespace, name)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Intercept topic create/delete to emit lifecycle events (RFC 0009).
	if len(parts) == 7 && parts[6] == "topics" && r.Method == http.MethodPost {
		m.createTopicDirect(w, r, ws, namespace)
		return
	}
	if len(parts) == 8 && parts[6] == "topics" && r.Method == http.MethodDelete {
		m.deleteTopicDirect(w, r, ws, namespace, parts[7])
		return
	}
	if len(parts) == 9 && parts[6] == "topics" && parts[8] == "events" {
		m.handleTopicEvents(w, r, ws, parts[7])
		return
	}
	if m.handleWorkspaceToolsRoute(w, r, ws, parts[6:]) {
		return
	}

	runtimePath := "/ws/" + strings.Join(parts[6:], "/")
	m.proxyRuntime(w, r, ws, runtimePath)
}

func (m *Manager) createWorkspace(w http.ResponseWriter, r *http.Request, namespace string) {
	var req workspaceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := validateName(req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	topics, err := decodeTopics(req.Topics)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	localToolNames := []string(nil)
	if req.Runtime != nil {
		localToolNames = req.Runtime.LocalTools
	}
	localTools, err := ResolveLocalTools(m.localTools, localToolNames)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	key := workspaceKey{namespace: namespace, name: req.Name}

	m.mu.Lock()
	if _, exists := m.workspaces[key]; exists {
		m.mu.Unlock()
		http.Error(w, "workspace already exists", http.StatusConflict)
		return
	}
	m.mu.Unlock()

	spec, err := m.launcher.WorkspacePaths(namespace, req.Name)
	if err != nil {
		m.logger.Error("failed to build workspace paths", "namespace", namespace, "name", req.Name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	hasState, err := stateDirHasEntries(spec.StateDir)
	if err != nil {
		m.logger.Error("failed to inspect workspace state", "namespace", namespace, "name", req.Name, "path", spec.StateDir, "error", err)
		http.Error(w, "failed to inspect workspace state", http.StatusInternalServerError)
		return
	}
	if hasState {
		http.Error(w, "workspace state already exists", http.StatusConflict)
		return
	}
	if err := m.bindManagerProxyAccess(namespace, req.Name, &spec); err != nil {
		m.logger.Error("failed to prepare manager proxy access", "namespace", namespace, "name", req.Name, "error", err)
		http.Error(w, "failed to prepare workspace manager access", http.StatusInternalServerError)
		return
	}
	spec.LocalTools = append([]LocalTool(nil), localTools...)
	if err := seedWorkspaceTemplate(spec.WorkspaceDir, req.Template); err != nil {
		m.logger.Error("failed to seed workspace template", "namespace", namespace, "name", req.Name, "template", req.Template, "error", err)
		http.Error(w, "failed to prepare workspace template", http.StatusInternalServerError)
		return
	}
	if err := writeWorkspaceLocalToolGuidance(spec.WorkspaceDir, localTools); err != nil {
		m.logger.Error("failed to write workspace local tool guidance", "namespace", namespace, "name", req.Name, "error", err)
		http.Error(w, "failed to prepare workspace guidance", http.StatusInternalServerError)
		return
	}
	runtime, err := m.launcher.Launch(r.Context(), spec)
	if err != nil {
		m.logger.Error("failed to launch workspace runtime", "namespace", namespace, "name", req.Name, "error", err)
		http.Error(w, "failed to launch workspace runtime", http.StatusBadGateway)
		return
	}
	ws := &Workspace{
		Namespace:  namespace,
		Name:       req.Name,
		StateDir:   spec.StateDir,
		CreatedAt:  time.Now().UTC(),
		Template:   strings.TrimSpace(req.Template),
		LocalTools: append([]LocalTool(nil), localTools...),
		Runtime:    *runtime,
	}

	if err := m.ensureTopics(r.Context(), ws, topics); err != nil {
		if runtime.Stop != nil {
			_ = runtime.Stop(context.Background())
		}
		m.logger.Error("failed to precreate runtime topics", "namespace", namespace, "name", req.Name, "error", err)
		http.Error(w, "failed to initialize runtime topics", http.StatusBadGateway)
		return
	}

	m.mu.Lock()
	m.workspaces[key] = ws
	m.mu.Unlock()
	if err := m.persistWorkspaceMetadata(ws); err != nil {
		m.logger.Error("failed to persist workspace metadata", "namespace", namespace, "name", req.Name, "error", err)
	}

	// Emit lifecycle events (RFC 0009).
	topicRefs := make([]map[string]string, 0, len(topics))
	for _, t := range topics {
		topicRefs = append(topicRefs, map[string]string{"name": t})
	}
	m.events.emit(namespace, "workspace_created", map[string]any{
		"workspace": map[string]any{
			"name":      ws.Name,
			"status":    "running",
			"template":  ws.Template,
			"createdAt": ws.CreatedAt.Format(time.RFC3339),
			"topics":    topicRefs,
		},
	})
	writeJSON(w, http.StatusCreated, m.specDetail(r, ws, topics))
}

func (m *Manager) deleteWorkspace(w http.ResponseWriter, r *http.Request, namespace, name string) {
	key := workspaceKey{namespace: namespace, name: name}
	m.mu.Lock()
	ws, ok := m.workspaces[key]
	if ok {
		delete(m.workspaces, key)
	}
	m.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Emit lifecycle event (RFC 0009) before stopping runtime.
	m.events.emit(namespace, "workspace_deleted", map[string]any{
		"workspace": map[string]any{"name": name},
	})

	if ws.Runtime.Stop != nil {
		if err := ws.Runtime.Stop(r.Context()); err != nil {
			m.logger.Error("failed to stop workspace runtime", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "failed to stop workspace runtime", http.StatusBadGateway)
			return
		}
	}
	if ws.StateDir != "" {
		if err := os.RemoveAll(ws.StateDir); err != nil {
			m.logger.Error("failed to remove workspace state", "namespace", namespace, "name", name, "path", ws.StateDir, "error", err)
			http.Error(w, "failed to remove workspace state", http.StatusInternalServerError)
			return
		}
	}
	if err := m.deleteWorkspaceManagerState(namespace, name); err != nil {
		m.logger.Error("failed to remove workspace manager state", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "failed to remove workspace manager state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":   name,
		"status": "deleted",
	})
}

func (m *Manager) updateWorkspace(w http.ResponseWriter, r *http.Request, namespace, name string) {
	ws, ok := m.getWorkspace(namespace, name)
	if !ok {
		http.NotFound(w, r)
		return
	}

	var req workspacePatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	currentNames := selectedLocalToolNames(ws.LocalTools)
	nextNames := currentNames
	if req.Runtime != nil && req.Runtime.LocalTools != nil {
		nextNames = append([]string(nil), req.Runtime.LocalTools...)
	}
	localTools, err := ResolveLocalTools(m.localTools, nextNames)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if sameLocalToolSelection(ws.LocalTools, localTools) {
		m.writeSpecDetail(w, r, ws)
		return
	}

	spec, err := m.launcher.WorkspacePaths(namespace, name)
	if err != nil {
		m.logger.Error("failed to build workspace paths", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := m.bindManagerProxyAccess(namespace, name, &spec); err != nil {
		m.logger.Error("failed to prepare manager proxy access", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "failed to prepare workspace manager access", http.StatusInternalServerError)
		return
	}
	spec.LocalTools = append([]LocalTool(nil), localTools...)
	if err := writeWorkspaceLocalToolGuidance(spec.WorkspaceDir, localTools); err != nil {
		m.logger.Error("failed to write workspace local tool guidance", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "failed to prepare workspace guidance", http.StatusInternalServerError)
		return
	}

	previousRuntime := ws.Runtime
	previousLocalTools := append([]LocalTool(nil), ws.LocalTools...)
	if previousRuntime.Stop != nil {
		if err := previousRuntime.Stop(r.Context()); err != nil {
			m.logger.Error("failed to stop workspace runtime for patch", "namespace", namespace, "name", name, "error", err)
			http.Error(w, "failed to stop workspace runtime", http.StatusBadGateway)
			return
		}
	}

	runtime, launchErr := m.launcher.Launch(r.Context(), spec)
	if launchErr != nil {
		m.logger.Error("failed to relaunch patched workspace runtime", "namespace", namespace, "name", name, "error", launchErr)
		if rollbackErr := m.restoreWorkspaceRuntime(r.Context(), ws, previousLocalTools); rollbackErr != nil {
			m.logger.Error("failed to restore workspace runtime after patch failure", "namespace", namespace, "name", name, "error", rollbackErr)
		}
		http.Error(w, "failed to relaunch workspace runtime", http.StatusBadGateway)
		return
	}

	m.mu.Lock()
	ws.LocalTools = append([]LocalTool(nil), localTools...)
	ws.Runtime = *runtime
	m.mu.Unlock()
	if err := m.persistWorkspaceMetadata(ws); err != nil {
		m.logger.Error("failed to persist patched workspace metadata", "namespace", namespace, "name", name, "error", err)
	}
	m.writeSpecDetail(w, r, ws)
}

func (m *Manager) restoreWorkspaceRuntime(ctx context.Context, ws *Workspace, localTools []LocalTool) error {
	spec, err := m.launcher.WorkspacePaths(ws.Namespace, ws.Name)
	if err != nil {
		return err
	}
	if err := m.bindManagerProxyAccess(ws.Namespace, ws.Name, &spec); err != nil {
		return err
	}
	spec.LocalTools = append([]LocalTool(nil), localTools...)
	if err := writeWorkspaceLocalToolGuidance(spec.WorkspaceDir, localTools); err != nil {
		return err
	}
	runtime, err := m.launcher.Launch(ctx, spec)
	if err != nil {
		return err
	}
	m.mu.Lock()
	ws.LocalTools = append([]LocalTool(nil), localTools...)
	ws.Runtime = *runtime
	m.mu.Unlock()
	return nil
}

func selectedLocalToolNames(tools []LocalTool) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func sameLocalToolSelection(left, right []LocalTool) bool {
	return slices.Equal(selectedLocalToolNames(left), selectedLocalToolNames(right))
}

func (m *Manager) writeSpecList(w http.ResponseWriter, r *http.Request, namespace string) {
	workspaces := m.listWorkspaces(namespace)
	resp := make([]workspaceSummary, 0, len(workspaces))
	for _, ws := range workspaces {
		resp = append(resp, m.specSummary(r, ws))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (m *Manager) writeSpecDetail(w http.ResponseWriter, r *http.Request, ws *Workspace) {
	writeJSON(w, http.StatusOK, m.specDetail(r, ws, nil))
}

func (m *Manager) ensureTopics(ctx context.Context, ws *Workspace, topics []string) error {
	for _, topic := range topics {
		body, _ := json.Marshal(map[string]string{"name": topic})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ws.Runtime.APIBase.String()+"/ws/topics", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		applyWorkspaceRuntimeIdentity(req, ctx)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusConflict {
			return fmt.Errorf("precreate topic %q: unexpected status %d", topic, res.StatusCode)
		}
	}
	return nil
}

func (m *Manager) createTopicDirect(w http.ResponseWriter, r *http.Request, ws *Workspace, namespace string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, ws.Runtime.APIBase.String()+"/ws/topics", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	applyWorkspaceRuntimeIdentity(req, r.Context())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "runtime unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	if resp.StatusCode == http.StatusCreated {
		var created struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(respBody, &created) == nil && created.Name != "" {
			m.events.emit(namespace, "topic_created", map[string]any{
				"workspace": ws.Name,
				"topic":     map[string]any{"name": created.Name},
			})
		}
	}
}

func (m *Manager) deleteTopicDirect(w http.ResponseWriter, r *http.Request, ws *Workspace, namespace, topicName string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodDelete, ws.Runtime.APIBase.String()+"/ws/topics/"+topicName, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	applyWorkspaceRuntimeIdentity(req, r.Context())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "runtime unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		m.events.emit(namespace, "topic_deleted", map[string]any{
			"workspace": ws.Name,
			"topic":     map[string]any{"name": topicName},
		})
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func (m *Manager) runtimeTopics(ctx context.Context, ws *Workspace) []runtimeTopicInfo {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ws.Runtime.APIBase.String()+"/ws/topics", nil)
	if err != nil {
		return nil
	}
	applyWorkspaceRuntimeIdentity(req, ctx)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		io.Copy(io.Discard, res.Body)
		return nil
	}
	var topics []runtimeTopicInfo
	if err := json.NewDecoder(res.Body).Decode(&topics); err != nil {
		return nil
	}
	slices.SortFunc(topics, func(a, b runtimeTopicInfo) int {
		return strings.Compare(a.Name, b.Name)
	})
	return topics
}

func (m *Manager) proxyRuntime(w http.ResponseWriter, r *http.Request, ws *Workspace, runtimePath string) {
	target := *ws.Runtime.APIBase
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = cleanProxyPath(runtimePath)
			req.URL.RawPath = req.URL.Path
			applyWorkspaceRuntimeIdentity(req, r.Context())
			// Browser websocket clients send an Origin for the manager host. Rewrite it
			// to the private runtime origin so the runtime's same-origin websocket check
			// accepts proxied topic connections.
			if origin := strings.TrimSpace(req.Header.Get("Origin")); origin != "" {
				req.Header.Set("X-Forwarded-Origin", origin)
				req.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			m.logger.Error("runtime proxy failed", "workspace", ws.Name, "path", runtimePath, "error", err)
			http.Error(rw, "runtime unavailable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func (m *Manager) getWorkspace(namespace, name string) (*Workspace, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ws, ok := m.workspaces[workspaceKey{namespace: namespace, name: name}]
	return ws, ok
}

func (m *Manager) listWorkspaces(namespace string) []*Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	workspaces := make([]*Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		if ws.Namespace == namespace {
			workspaces = append(workspaces, ws)
		}
	}
	slices.SortFunc(workspaces, func(a, b *Workspace) int {
		return strings.Compare(a.Name, b.Name)
	})
	return workspaces
}

func (m *Manager) specSummary(r *http.Request, ws *Workspace) workspaceSummary {
	return workspaceSummary{
		ID:        workspaceID(ws),
		Namespace: ws.Namespace,
		Name:      ws.Name,
		Status:    m.runtimeStatus(r.Context(), ws),
		API:       m.specAPIBase(r, ws),
		CreatedAt: ws.CreatedAt.Format(time.RFC3339),
	}
}

func (m *Manager) specDetail(r *http.Request, ws *Workspace, topics []string) workspaceDetail {
	if topics == nil {
		topicInfos := m.runtimeTopics(r.Context(), ws)
		topics = make([]string, 0, len(topicInfos))
		for _, topic := range topicInfos {
			topics = append(topics, topic.Name)
		}
	}
	resp := workspaceDetail{
		workspaceSummary: workspaceSummary{
			ID:        workspaceID(ws),
			Namespace: ws.Namespace,
			Name:      ws.Name,
			Status:    m.runtimeStatus(r.Context(), ws),
			API:       m.specAPIBase(r, ws),
			CreatedAt: ws.CreatedAt.Format(time.RFC3339),
		},
	}
	if len(ws.LocalTools) > 0 {
		resp.Runtime = &workspaceRuntimeInfo{LocalTools: localToolInfos(ws.LocalTools)}
	}
	for _, topic := range topics {
		resp.Topics = append(resp.Topics, workspaceTopicRef{
			Name:    topic,
			Events:  m.specTopicEventsURL(r, ws, topic),
			Shelley: m.specShelleyURL(r, ws, topic),
		})
	}
	return resp
}

func (m *Manager) runtimeStatus(ctx context.Context, ws *Workspace) string {
	if ws.Runtime.Health == nil {
		return "running"
	}
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ws.Runtime.Health(healthCtx); err != nil {
		return "unavailable"
	}
	return "running"
}

func (m *Manager) specAPIBase(r *http.Request, ws *Workspace) string {
	return requestBase(r, false) + "/apis/v1/namespaces/" + ws.Namespace + "/workspaces/" + ws.Name
}

func (m *Manager) specTopicEventsURL(r *http.Request, ws *Workspace, topic string) string {
	return m.specAPIBase(r, ws) + "/topics/" + url.PathEscape(topic) + "/events"
}

func normalizeShelleyUIMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "disabled":
		return "disabled"
	case "same_host_port":
		return "same_host_port"
	default:
		return ""
	}
}

func (m *Manager) specShelleyURL(r *http.Request, ws *Workspace, topic string) string {
	switch m.shelleyUIMode {
	case "disabled":
		return ""
	case "same_host_port":
		return sameHostPortShelleyURL(r, ws, topic)
	default:
		return ""
	}
}

func decodeTopics(raw json.RawMessage) ([]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		for _, name := range names {
			if err := validateName(name); err != nil {
				return nil, fmt.Errorf("invalid topic name %q: %w", name, err)
			}
		}
		return names, nil
	}
	var topics []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &topics); err != nil {
		return nil, errors.New("topics must be an array of strings or objects with name")
	}
	names = nil
	for _, topic := range topics {
		if err := validateName(topic.Name); err != nil {
			return nil, fmt.Errorf("invalid topic name %q: %w", topic.Name, err)
		}
		names = append(names, topic.Name)
	}
	return names, nil
}

func requestBase(r *http.Request, websocket bool) string {
	scheme := "http"
	if websocket {
		scheme = "ws"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		switch forwarded {
		case "https":
			if websocket {
				scheme = "wss"
			} else {
				scheme = "https"
			}
		case "wss":
			if websocket {
				scheme = "wss"
			}
		case "ws":
			if websocket {
				scheme = "ws"
			}
		case "http":
			if websocket {
				scheme = "ws"
			}
		}
	} else if r.TLS != nil {
		if websocket {
			scheme = "wss"
		} else {
			scheme = "https"
		}
	}
	return scheme + "://" + requestHost(r)
}

func requestHost(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx >= 0 {
			forwarded = forwarded[:idx]
		}
		if forwarded = strings.TrimSpace(forwarded); forwarded != "" {
			return forwarded
		}
	}
	return r.Host
}

func sameHostPortShelleyURL(r *http.Request, ws *Workspace, topic string) string {
	if ws == nil || ws.Runtime.APIBase == nil {
		return ""
	}
	runtimePort := strings.TrimSpace(ws.Runtime.APIBase.Port())
	if runtimePort == "" {
		return ""
	}
	publicHost := requestHost(r)
	if publicHost == "" {
		return ""
	}
	baseURL, err := url.Parse(requestBase(r, false))
	if err != nil {
		return ""
	}
	hostname := baseURL.Hostname()
	if hostname == "" {
		return ""
	}
	baseURL.Host = net.JoinHostPort(hostname, runtimePort)
	basePath := ws.Runtime.APIBase.Path
	if strings.TrimSpace(topic) == "" {
		baseURL.Path = cleanProxyPath(basePath)
	} else {
		baseURL.Path = cleanProxyPath(path.Join(basePath, "c", topic))
	}
	baseURL.RawPath = baseURL.Path
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	return baseURL.String()
}

func workspaceID(ws *Workspace) string {
	return ws.Name + "." + ws.Namespace + "@shelleymanager"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func cleanProxyPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(p, "/"))
}

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name required")
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return fmt.Errorf("invalid name %q", name)
	}
	return nil
}
