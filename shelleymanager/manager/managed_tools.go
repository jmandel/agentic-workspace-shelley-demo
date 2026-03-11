package manager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const managerPrivateStateDir = ".manager"

type managedToolBinding struct {
	ToolName           string                  `json:"toolName"`
	Protocol           string                  `json:"protocol"`
	Transport          json.RawMessage         `json:"transport,omitempty"`
	ExecutionTransport json.RawMessage         `json:"executionTransport,omitempty"`
	Public             *managedToolPublicState `json:"public,omitempty"`
}

type managedToolPublicState struct {
	Description   string          `json:"description,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	CredentialRef string          `json:"credentialRef,omitempty"`
	Transport     json.RawMessage `json:"transport,omitempty"`
	Config        json.RawMessage `json:"config,omitempty"`
}

type managerToolCreateRequest struct {
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Protocol      string          `json:"protocol,omitempty"`
	Actions       json.RawMessage `json:"actions,omitempty"`
	Tools         json.RawMessage `json:"tools,omitempty"`
	Transport     json.RawMessage `json:"transport,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	CredentialRef string          `json:"credentialRef,omitempty"`
	Config        json.RawMessage `json:"config,omitempty"`
}

type managedMCPTransportConfig struct {
	Transport            string            `json:"transport"`
	Type                 string            `json:"type"`
	Command              string            `json:"command"`
	Args                 []string          `json:"args"`
	Env                  map[string]string `json:"env"`
	Cwd                  string            `json:"cwd"`
	Endpoint             string            `json:"endpoint"`
	URL                  string            `json:"url"`
	Headers              map[string]string `json:"headers"`
	DisableStandaloneSSE bool              `json:"disableStandaloneSSE"`
	MaxRetries           *int              `json:"maxRetries,omitempty"`
}

type managerProxyInvokeRequest struct {
	Action string          `json:"action"`
	Input  json.RawMessage `json:"input,omitempty"`
}

type managerProxyInvokeResponse struct {
	Content string `json:"content,omitempty"`
}

type managedToolHeaderRoundTripper struct {
	base    http.RoundTripper
	headers http.Header
}

func (t *managedToolHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	for name, values := range t.headers {
		for _, value := range values {
			clone.Header.Add(name, value)
		}
	}
	return t.base.RoundTrip(clone)
}

func (m *Manager) SetInternalBaseURL(base string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalBaseURL = strings.TrimSpace(base)
}

func (m *Manager) internalBase() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return strings.TrimSpace(m.internalBaseURL)
}

func (m *Manager) handleInternal(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/"))
	if len(parts) != 8 || parts[0] != "internal" || parts[1] != "namespaces" || parts[3] != "workspaces" || parts[5] != "tools" || parts[7] != "invoke" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	namespace := parts[2]
	name := parts[4]
	toolName := parts[6]
	if err := validateName(namespace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateName(name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateName(toolName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token, err := m.workspaceManagerToken(namespace, name)
	if err != nil {
		m.logger.Error("failed to read workspace manager token", "namespace", namespace, "workspace", name, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !managerProxyAuthorized(r, token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	binding, err := m.loadManagedToolBinding(namespace, name, toolName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		m.logger.Error("failed to load managed tool binding", "namespace", namespace, "workspace", name, "tool", toolName, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var req managerProxyInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	if req.Action == "" {
		http.Error(w, "action required", http.StatusBadRequest)
		return
	}

	content, err := m.invokeManagedTool(r.Context(), binding, req.Action, req.Input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, managerProxyInvokeResponse{Content: content})
}

func (m *Manager) handleWorkspaceToolsRoute(w http.ResponseWriter, r *http.Request, ws *Workspace, parts []string) bool {
	if len(parts) == 0 || parts[0] != "tools" {
		return false
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodPost:
		m.createManagedWorkspaceTool(w, r, ws)
		return true
	case len(parts) == 1 && r.Method == http.MethodGet:
		m.listManagedWorkspaceTools(w, r, ws)
		return true
	case len(parts) == 2 && (r.Method == http.MethodGet || r.Method == http.MethodDelete):
		m.handleManagedWorkspaceTool(w, r, ws, parts[1])
		return true
	default:
		return false
	}
}

func (m *Manager) createManagedWorkspaceTool(w http.ResponseWriter, r *http.Request, ws *Workspace) {
	var req managerToolCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Protocol = strings.ToLower(strings.TrimSpace(req.Protocol))
	if req.Protocol == "" {
		req.Protocol = "mcp"
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if req.Protocol != "mcp" {
		http.Error(w, "unsupported protocol", http.StatusBadRequest)
		return
	}

	executionTransport, err := m.resolveManagedToolTransport(ws, req.Transport)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// If no tools provided, discover them from the MCP server.
	toolsPayload := req.Tools
	if len(toolsPayload) == 0 {
		toolsPayload = req.Actions
	}
	if len(toolsPayload) == 0 && req.Protocol == "mcp" {
		binding := managedToolBinding{
			ToolName:           req.Name,
			Protocol:           req.Protocol,
			ExecutionTransport: executionTransport,
		}
		discovered, err := m.listMCPToolDefs(r.Context(), binding)
		if err != nil {
			m.logger.Error("MCP tool discovery failed at registration", "tool", req.Name, "error", err)
			// Continue without tools — tool will be registered but not exposed to the LLM
		} else if len(discovered) > 0 {
			toolsPayload, _ = json.Marshal(discovered)
		}
	}

	createPayloadMap := map[string]any{
		"name":        req.Name,
		"description": req.Description,
		"provider":    req.Provider,
		"protocol":    req.Protocol,
		"transport": map[string]any{
			"type": "manager_proxy",
		},
	}
	if len(toolsPayload) > 0 {
		createPayloadMap["tools"] = json.RawMessage(toolsPayload)
	}
	createPayload, err := json.Marshal(createPayloadMap)
	if err != nil {
		http.Error(w, "failed to encode tool registration", http.StatusInternalServerError)
		return
	}
	status, headers, body, err := m.runtimeJSONRequest(r.Context(), ws, http.MethodPost, "/ws/tools", createPayload)
	if err != nil {
		http.Error(w, "runtime unavailable", http.StatusBadGateway)
		return
	}
	if status != http.StatusCreated {
		copyHeaders(w.Header(), headers)
		w.WriteHeader(status)
		w.Write(body)
		return
	}

	binding := managedToolBinding{
		ToolName:           req.Name,
		Protocol:           req.Protocol,
		ExecutionTransport: executionTransport,
		Public: &managedToolPublicState{
			Description:   req.Description,
			Provider:      req.Provider,
			CredentialRef: req.CredentialRef,
			Transport:     cloneRawJSON(req.Transport),
			Config:        cloneRawJSON(req.Config),
		},
	}
	if err := m.saveManagedToolBinding(ws.Namespace, ws.Name, binding); err != nil {
		_, _, _, _ = m.runtimeJSONRequest(r.Context(), ws, http.MethodDelete, "/ws/tools/"+req.Name, nil)
		http.Error(w, "failed to persist managed tool binding", http.StatusInternalServerError)
		return
	}

	m.writeManagedWorkspaceToolDetail(w, r, ws, req.Name, http.StatusCreated)
}

func (m *Manager) listManagedWorkspaceTools(w http.ResponseWriter, r *http.Request, ws *Workspace) {
	status, headers, body, err := m.runtimeJSONRequest(r.Context(), ws, http.MethodGet, "/ws/tools", nil)
	if err != nil {
		http.Error(w, "runtime unavailable", http.StatusBadGateway)
		return
	}
	if status != http.StatusOK {
		copyHeaders(w.Header(), headers)
		w.WriteHeader(status)
		w.Write(body)
		return
	}
	var tools []map[string]any
	if err := json.Unmarshal(body, &tools); err != nil {
		http.Error(w, "invalid runtime response", http.StatusBadGateway)
		return
	}
	inventory := make([]map[string]any, 0, len(ws.LocalTools)+len(tools))
	for _, tool := range ws.LocalTools {
		inventory = append(inventory, map[string]any{
			"kind":        "local",
			"name":        tool.Name,
			"description": tool.Description,
		})
	}
	for _, tool := range tools {
		if name, _ := tool["name"].(string); name != "" {
			if binding, err := m.loadManagedToolBinding(ws.Namespace, ws.Name, name); err == nil {
				overlayManagedToolPublic(tool, binding)
			}
			entry := map[string]any{
				"kind": "mcp",
				"name": name,
			}
			if description, _ := tool["description"].(string); description != "" {
				entry["description"] = description
			}
			inventory = append(inventory, entry)
		}
	}
	writeJSON(w, http.StatusOK, inventory)
}

func (m *Manager) handleManagedWorkspaceTool(w http.ResponseWriter, r *http.Request, ws *Workspace, toolName string) {
	switch r.Method {
	case http.MethodGet:
		m.writeManagedWorkspaceToolDetail(w, r, ws, toolName, http.StatusOK)
	case http.MethodDelete:
		status, headers, body, err := m.runtimeJSONRequest(r.Context(), ws, http.MethodDelete, "/ws/tools/"+toolName, nil)
		if err != nil {
			http.Error(w, "runtime unavailable", http.StatusBadGateway)
			return
		}
		if status >= 200 && status < 300 || status == http.StatusNotFound {
			if err := m.deleteManagedToolBinding(ws.Namespace, ws.Name, toolName); err != nil {
				http.Error(w, "failed to delete managed tool binding", http.StatusInternalServerError)
				return
			}
		}
		copyHeaders(w.Header(), headers)
		w.WriteHeader(status)
		w.Write(body)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *Manager) writeManagedWorkspaceToolDetail(w http.ResponseWriter, r *http.Request, ws *Workspace, toolName string, statusOverride int) {
	status, headers, body, err := m.runtimeJSONRequest(r.Context(), ws, http.MethodGet, "/ws/tools/"+toolName, nil)
	if err != nil {
		http.Error(w, "runtime unavailable", http.StatusBadGateway)
		return
	}
	if status != http.StatusOK {
		copyHeaders(w.Header(), headers)
		w.WriteHeader(status)
		w.Write(body)
		return
	}
	var tool map[string]any
	if err := json.Unmarshal(body, &tool); err != nil {
		http.Error(w, "invalid runtime response", http.StatusBadGateway)
		return
	}
	if binding, err := m.loadManagedToolBinding(ws.Namespace, ws.Name, toolName); err == nil {
		overlayManagedToolPublic(tool, binding)
	}
	writeJSON(w, statusOverride, tool)
}

func managerProxyAuthorized(r *http.Request, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if value, ok := strings.CutPrefix(auth, "Bearer "); ok && strings.TrimSpace(value) == token {
		return true
	}
	return strings.TrimSpace(r.Header.Get("X-Workspace-Token")) == token
}

type mcpToolDef struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

func (m *Manager) listMCPToolDefs(ctx context.Context, binding managedToolBinding) ([]mcpToolDef, error) {
	cfg, err := decodeManagedMCPTransport(binding)
	if err != nil {
		return nil, err
	}
	transport, err := m.newManagedMCPTransport(ctx, cfg)
	if err != nil {
		return nil, err
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "shelleymanager", Version: "workspace"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect mcp tool %s for discovery: %w", binding.ToolName, err)
	}
	defer session.Close()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list mcp tools for %s: %w", binding.ToolName, err)
	}

	defs := make([]mcpToolDef, 0, len(result.Tools))
	for _, tool := range result.Tools {
		def := mcpToolDef{
			Name:        tool.Name,
			Title:       tool.Title,
			Description: tool.Description,
		}
		if tool.InputSchema != nil {
			schemaJSON, err := json.Marshal(tool.InputSchema)
			if err == nil {
				def.InputSchema = schemaJSON
			}
		}
		defs = append(defs, def)
	}
	return defs, nil
}

func (m *Manager) invokeManagedTool(ctx context.Context, binding managedToolBinding, action string, input json.RawMessage) (string, error) {
	switch strings.ToLower(strings.TrimSpace(binding.Protocol)) {
	case "", "mcp":
		return m.invokeManagedMCPTool(ctx, binding, action, input)
	default:
		return "", fmt.Errorf("unsupported managed tool protocol %q", binding.Protocol)
	}
}

func (m *Manager) invokeManagedMCPTool(ctx context.Context, binding managedToolBinding, action string, input json.RawMessage) (string, error) {
	cfg, err := decodeManagedMCPTransport(binding)
	if err != nil {
		return "", err
	}
	transport, err := m.newManagedMCPTransport(ctx, cfg)
	if err != nil {
		return "", err
	}
	args, err := managedToolArguments(input)
	if err != nil {
		return "", err
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "shelleymanager", Version: "workspace"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return "", fmt.Errorf("connect managed mcp tool %s: %w", binding.ToolName, err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      action,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("call managed mcp tool %s/%s: %w", binding.ToolName, action, err)
	}
	if result == nil {
		return "", nil
	}
	if result.IsError {
		return "", fmt.Errorf("managed mcp tool %s/%s failed: %s", binding.ToolName, action, summarizeManagedMCPResult(result))
	}
	return managedMCPResultToText(result)
}

func decodeManagedMCPTransport(binding managedToolBinding) (managedMCPTransportConfig, error) {
	var cfg managedMCPTransportConfig
	payload := binding.executionTransportPayload()
	if len(payload) == 0 || strings.TrimSpace(string(payload)) == "" {
		return cfg, fmt.Errorf("managed tool %s is missing transport", binding.ToolName)
	}
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid transport for managed tool %s: %w", binding.ToolName, err)
	}
	cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Transport))
	if cfg.Transport == "" {
		cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Type))
	}
	switch cfg.Transport {
	case "stdio":
		if strings.TrimSpace(cfg.Command) == "" {
			return cfg, fmt.Errorf("managed tool %s stdio transport requires command", binding.ToolName)
		}
	case "streamable_http", "streamable-http":
		cfg.Transport = "streamable_http"
		if strings.TrimSpace(cfg.Endpoint) == "" {
			cfg.Endpoint = strings.TrimSpace(cfg.URL)
		}
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return cfg, fmt.Errorf("managed tool %s streamable_http transport requires endpoint", binding.ToolName)
		}
	default:
		return cfg, fmt.Errorf("unsupported managed tool transport %q", cfg.Transport)
	}
	return cfg, nil
}

func (b managedToolBinding) executionTransportPayload() json.RawMessage {
	if len(b.ExecutionTransport) > 0 {
		return cloneRawJSON(b.ExecutionTransport)
	}
	return cloneRawJSON(b.Transport)
}

func (b managedToolBinding) publicTransportPayload() json.RawMessage {
	if b.Public != nil && len(b.Public.Transport) > 0 {
		return cloneRawJSON(b.Public.Transport)
	}
	return cloneRawJSON(b.Transport)
}

func (m *Manager) newManagedMCPTransport(ctx context.Context, cfg managedMCPTransportConfig) (mcp.Transport, error) {
	switch cfg.Transport {
	case "stdio":
		commandPath, err := resolveManagedMCPCommand(cfg.Command)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, commandPath, cfg.Args...)
		if cwd := strings.TrimSpace(cfg.Cwd); cwd != "" {
			cmd.Dir = cwd
		}
		if len(cfg.Env) > 0 {
			cmd.Env = mergeManagedMCPEnv(os.Environ(), cfg.Env)
		}
		cmd.Stderr = os.Stderr
		return &mcp.CommandTransport{Command: cmd}, nil
	case "streamable_http":
		transport := &mcp.StreamableClientTransport{
			Endpoint:             cfg.Endpoint,
			DisableStandaloneSSE: cfg.DisableStandaloneSSE,
		}
		if cfg.MaxRetries != nil {
			transport.MaxRetries = *cfg.MaxRetries
		}
		if len(cfg.Headers) > 0 {
			transport.HTTPClient = &http.Client{
				Transport: &managedToolHeaderRoundTripper{
					base:    http.DefaultTransport,
					headers: managedMCPHeaders(cfg.Headers),
				},
			}
		}
		return transport, nil
	default:
		return nil, fmt.Errorf("unsupported managed tool transport %q", cfg.Transport)
	}
}

func resolveManagedMCPCommand(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing managed mcp command")
	}
	if filepath.IsAbs(raw) || strings.ContainsRune(raw, filepath.Separator) {
		return raw, nil
	}
	resolved, err := exec.LookPath(raw)
	if err != nil {
		return "", fmt.Errorf("resolve managed mcp command %q: %w", raw, err)
	}
	return resolved, nil
}

func mergeManagedMCPEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(overrides))
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if value, exists := overrides[name]; exists {
			env = append(env, name+"="+value)
			seen[name] = struct{}{}
			continue
		}
		env = append(env, entry)
	}
	for name, value := range overrides {
		if _, ok := seen[name]; ok {
			continue
		}
		env = append(env, name+"="+value)
	}
	return env
}

func managedMCPHeaders(headers map[string]string) http.Header {
	result := make(http.Header, len(headers))
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		result.Add(name, value)
	}
	return result
}

func managedToolArguments(raw json.RawMessage) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid managed tool input payload: %w", err)
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func managedMCPResultToText(result *mcp.CallToolResult) (string, error) {
	if result == nil {
		return "", nil
	}
	var parts []string
	for _, item := range result.Content {
		text, err := managedMCPContentToText(item)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	if result.StructuredContent != nil {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("marshal managed mcp structured content: %w", err)
		}
		if len(raw) > 0 && string(raw) != "null" {
			parts = append(parts, string(raw))
		}
	}
	return strings.Join(parts, "\n"), nil
}

func managedMCPContentToText(content mcp.Content) (string, error) {
	switch v := content.(type) {
	case *mcp.TextContent:
		return v.Text, nil
	default:
		raw, err := json.Marshal(content)
		if err != nil {
			return "", fmt.Errorf("marshal managed mcp content: %w", err)
		}
		return string(raw), nil
	}
}

func summarizeManagedMCPResult(result *mcp.CallToolResult) string {
	text, err := managedMCPResultToText(result)
	if err == nil && strings.TrimSpace(text) != "" {
		return text
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr == nil {
		return string(raw)
	}
	return "tool returned an error"
}

func (m *Manager) runtimeJSONRequest(ctx context.Context, ws *Workspace, method, runtimePath string, body []byte) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, ws.Runtime.APIBase.String()+runtimePath, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	applyWorkspaceRuntimeIdentity(req, ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Clone(), respBody, nil
}

func overlayManagedToolPublic(tool map[string]any, binding managedToolBinding) {
	if tool == nil {
		return
	}
	if protocol := strings.TrimSpace(binding.Protocol); protocol != "" {
		tool["protocol"] = protocol
	}
	if transport := binding.publicTransportPayload(); len(transport) > 0 {
		var decoded any
		if err := json.Unmarshal(transport, &decoded); err == nil {
			decoded = redactTransportForRead(decoded)
			tool["transport"] = decoded
		}
	}
	if binding.Public == nil {
		return
	}
	if description := strings.TrimSpace(binding.Public.Description); description != "" {
		tool["description"] = description
	}
	if provider := strings.TrimSpace(binding.Public.Provider); provider != "" {
		tool["provider"] = provider
	}
	if credentialRef := strings.TrimSpace(binding.Public.CredentialRef); credentialRef != "" {
		tool["credentialRef"] = credentialRef
	}
	if len(binding.Public.Config) > 0 {
		var decoded any
		if err := json.Unmarshal(binding.Public.Config, &decoded); err == nil {
			tool["config"] = decoded
		}
	}
}

func redactTransportForRead(decoded any) any {
	transport, ok := decoded.(map[string]any)
	if !ok {
		return decoded
	}
	if env, ok := transport["env"].(map[string]any); ok {
		transport["env"] = redactNamedSecrets(env)
	}
	if headers, ok := transport["headers"].(map[string]any); ok {
		transport["headers"] = redactNamedSecrets(headers)
	}
	return transport
}

func redactNamedSecrets(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	redacted := make(map[string]any, len(values))
	for name := range values {
		redacted[name] = map[string]any{
			"redacted": true,
		}
	}
	return redacted
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func (m *Manager) resolveManagedToolTransport(ws *Workspace, raw json.RawMessage) (json.RawMessage, error) {
	var cfg managedMCPTransportConfig
	if len(raw) == 0 {
		return nil, fmt.Errorf("transport required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid transport: %w", err)
	}
	cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Transport))
	if cfg.Transport == "" {
		cfg.Transport = strings.ToLower(strings.TrimSpace(cfg.Type))
	}
	switch cfg.Transport {
	case "stdio":
		if strings.TrimSpace(cfg.Command) == "" {
			return nil, fmt.Errorf("stdio transport requires command")
		}
		cfg.Command = m.resolveManagerRuntimePath(ws, cfg.Command)
		for i, arg := range cfg.Args {
			cfg.Args[i] = m.resolveManagerRuntimePath(ws, arg)
		}
		for key, value := range cfg.Env {
			cfg.Env[key] = m.resolveManagerRuntimePath(ws, value)
		}
		cfg.Cwd = m.resolveManagerWorkingDir(ws, cfg.Cwd)
	case "streamable_http", "streamable-http":
		cfg.Transport = "streamable_http"
		if strings.TrimSpace(cfg.Endpoint) == "" {
			cfg.Endpoint = strings.TrimSpace(cfg.URL)
		}
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return nil, fmt.Errorf("streamable_http transport requires endpoint")
		}
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	return json.Marshal(cfg)
}

func (m *Manager) resolveManagerWorkingDir(ws *Workspace, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if filepath.IsAbs(raw) {
		return m.resolveManagerRuntimePath(ws, raw)
	}
	return filepath.Join(m.workspaceHostDir(ws), raw)
}

func (m *Manager) resolveManagerRuntimePath(ws *Workspace, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if !strings.HasPrefix(raw, runtimeSharedToolsDir+"/") {
		return raw
	}

	rel := strings.TrimPrefix(raw, runtimeSharedToolsDir+"/")
	if rel == "" {
		return raw
	}
	if strings.HasPrefix(rel, "bin/") {
		commandName := path.Base(rel)
		for _, tool := range ws.LocalTools {
			for _, cmd := range tool.Commands {
				if cmd.Name == commandName {
					return filepath.Join(tool.HostRoot, filepath.Clean(cmd.RelativePath))
				}
			}
		}
		return raw
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return raw
	}
	toolName := parts[0]
	for _, tool := range ws.LocalTools {
		if tool.Name != toolName {
			continue
		}
		if len(parts) == 1 {
			return tool.HostRoot
		}
		return filepath.Join(tool.HostRoot, filepath.Join(parts[1:]...))
	}
	return raw
}

func (m *Manager) workspaceHostDir(ws *Workspace) string {
	if ws == nil {
		return ""
	}
	spec, err := m.launcher.WorkspacePaths(ws.Namespace, ws.Name)
	if err == nil && strings.TrimSpace(spec.WorkspaceDir) != "" {
		return spec.WorkspaceDir
	}
	return filepath.Join(ws.StateDir, "workspace")
}

func (m *Manager) privateStateDir() string {
	if strings.TrimSpace(m.stateRoot) == "" {
		return ""
	}
	return filepath.Join(m.stateRoot, managerPrivateStateDir)
}

func (m *Manager) workspaceManagerDir(namespace, name string) string {
	return filepath.Join(m.privateStateDir(), namespace, name)
}

func (m *Manager) workspaceManagerToken(namespace, name string) (string, error) {
	tokenPath := filepath.Join(m.workspaceManagerDir(namespace, name), "token")
	raw, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func (m *Manager) ensureWorkspaceManagerToken(namespace, name string) (string, error) {
	dir := m.workspaceManagerDir(namespace, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tokenPath := filepath.Join(dir, "token")
	if raw, err := os.ReadFile(tokenPath); err == nil {
		return strings.TrimSpace(string(raw)), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func (m *Manager) managedToolBindingPath(namespace, name, tool string) string {
	return filepath.Join(m.workspaceManagerDir(namespace, name), "tools", tool+".json")
}

func (m *Manager) saveManagedToolBinding(namespace, name string, binding managedToolBinding) error {
	dir := filepath.Dir(m.managedToolBindingPath(namespace, name, binding.ToolName))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.managedToolBindingPath(namespace, name, binding.ToolName), raw, 0o600)
}

func (m *Manager) loadManagedToolBinding(namespace, name, tool string) (managedToolBinding, error) {
	var binding managedToolBinding
	raw, err := os.ReadFile(m.managedToolBindingPath(namespace, name, tool))
	if err != nil {
		return binding, err
	}
	if err := json.Unmarshal(raw, &binding); err != nil {
		return binding, err
	}
	return binding, nil
}

func (m *Manager) deleteManagedToolBinding(namespace, name, tool string) error {
	err := os.Remove(m.managedToolBindingPath(namespace, name, tool))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) deleteWorkspaceManagerState(namespace, name string) error {
	dir := m.workspaceManagerDir(namespace, name)
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	err := os.RemoveAll(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) bindManagerProxyAccess(namespace, name string, spec *LaunchSpec) error {
	if spec == nil {
		return nil
	}
	spec.ManagerURL = m.internalBase()
	token, err := m.ensureWorkspaceManagerToken(namespace, name)
	if err != nil {
		return err
	}
	spec.ManagerToken = token
	return nil
}
