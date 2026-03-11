package manager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/golang-jwt/jwt/v5"
)

type fakeLauncher struct {
	mu       sync.Mutex
	runtime  *Runtime
	launches []LaunchSpec
	stops    int
	baseDir  string
}

func (f *fakeLauncher) Name() string { return "fake" }

func (f *fakeLauncher) WorkspacePaths(namespace, name string) (LaunchSpec, error) {
	base := "/tmp/" + namespace + "/" + name
	if f.baseDir != "" {
		base = filepath.Join(f.baseDir, namespace, name)
	}
	return LaunchSpec{
		Namespace:    namespace,
		Name:         name,
		StateDir:     base,
		WorkspaceDir: base + "/workspace",
		DBPath:       base + "/shelley.db",
	}, nil
}

func (f *fakeLauncher) Launch(_ context.Context, spec LaunchSpec) (*Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launches = append(f.launches, spec)
	if f.runtime == nil {
		return nil, nil
	}
	cloned := *f.runtime
	if f.runtime.Stop != nil {
		origStop := f.runtime.Stop
		cloned.Stop = func(ctx context.Context) error {
			f.mu.Lock()
			f.stops++
			f.mu.Unlock()
			return origStop(ctx)
		}
	}
	return &cloned, nil
}

func managerTestJWT(t *testing.T, subject string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss":  "manager-test",
		"sub":  subject,
		"name": subject,
		"iat":  time.Now().Unix(),
	})
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to sign test jwt: %v", err)
	}
	return signed
}

func setManagerAuth(t *testing.T, req *http.Request, subject string) {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+managerTestJWT(t, subject))
}

func authenticateManagerWS(t *testing.T, ctx context.Context, conn *websocket.Conn, subject string) {
	t.Helper()
	if err := wsjson.Write(ctx, conn, map[string]string{
		"type":  "authenticate",
		"token": managerTestJWT(t, subject),
	}); err != nil {
		t.Fatalf("failed to authenticate manager websocket: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read websocket auth response: %v", err)
	}
	if !strings.Contains(string(data), `"type":"authenticated"`) {
		t.Fatalf("unexpected websocket auth payload %s", data)
	}
}

func TestManagerCreateAndProxyRoutes(t *testing.T) {
	var seenPaths []string
	var mu sync.Mutex

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenPaths = append(seenPaths, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"general"}`)
		case r.URL.Path == "/ws/topics/general" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"general","clients":0,"queuedCount":0,"createdAt":"2026-03-10T00:00:00Z"}]`)
		case r.URL.Path == "/ws/files" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"node":{"path":"docs","name":"docs","kind":"directory","size":0,"modifiedAt":"2026-03-10T00:00:00Z"},"entries":[]}`)
		case r.URL.Path == "/ws/files/content" && r.Method == http.MethodGet:
			io.WriteString(w, "proxied-file")
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	runtimeURL, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}

	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createBody := `{"name":"demo","topics":[{"name":"general"}]}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, req, "alice@example.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", res.StatusCode)
	}

	var created workspaceDetail
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "demo" || created.API == "" {
		t.Fatalf("unexpected create response: %+v", created)
	}
	if len(created.Topics) != 1 || created.Topics[0].ActiveRun != nil || created.Topics[0].QueuedCount != 0 {
		t.Fatalf("expected create response to include idle topic summary, got %+v", created.Topics)
	}

	topicsReq, err := http.NewRequest(http.MethodGet, server.URL+"/apis/v1/namespaces/acme/workspaces/demo/topics", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, topicsReq, "alice@example.com")
	topicsRes, err := http.DefaultClient.Do(topicsReq)
	if err != nil {
		t.Fatal(err)
	}
	defer topicsRes.Body.Close()
	body, _ := io.ReadAll(topicsRes.Body)
	if !strings.Contains(string(body), "general") {
		t.Fatalf("expected proxied topics response, got %s", body)
	}

	filesReq, err := http.NewRequest(http.MethodGet, server.URL+"/apis/v1/namespaces/acme/workspaces/demo/files?path=docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, filesReq, "alice@example.com")
	filesRes, err := http.DefaultClient.Do(filesReq)
	if err != nil {
		t.Fatal(err)
	}
	defer filesRes.Body.Close()
	filesBody, _ := io.ReadAll(filesRes.Body)
	if !strings.Contains(string(filesBody), `"path":"docs"`) {
		t.Fatalf("unexpected proxied files body %q", filesBody)
	}

	fileReq, err := http.NewRequest(http.MethodGet, server.URL+"/apis/v1/namespaces/acme/workspaces/demo/files/content?path=readme.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, fileReq, "alice@example.com")
	fileRes, err := http.DefaultClient.Do(fileReq)
	if err != nil {
		t.Fatal(err)
	}
	defer fileRes.Body.Close()
	fileBody, _ := io.ReadAll(fileRes.Body)
	if string(fileBody) != "proxied-file" {
		t.Fatalf("unexpected proxied file body %q", fileBody)
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, server.URL+"/apis/v1/namespaces/acme/workspaces/demo/topics/general", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, deleteReq, "alice@example.com")
	deleteRes, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	deleteRes.Body.Close()
	if deleteRes.StatusCode != http.StatusNoContent {
		t.Fatalf("delete topic status = %d", deleteRes.StatusCode)
	}

	mu.Lock()
	got := strings.Join(seenPaths, "\n")
	mu.Unlock()
	for _, expected := range []string{
		"POST /ws/topics",
		"GET /ws/topics",
		"DELETE /ws/topics/general",
		"GET /ws/files?path=docs",
		"GET /ws/files/content?path=readme.txt",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected runtime to see %q, got:\n%s", expected, got)
		}
	}
}

func TestManagerRegistersWorkspaceToolAsManagedProxy(t *testing.T) {
	var createBodies []string
	var getBodies []string
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"bp-panel-validator"}`)
		case r.URL.Path == "/ws/tools" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			createBodies = append(createBodies, string(body))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"hl7-jira","protocol":"mcp","transport":{"type":"manager_proxy"}}`)
		case r.URL.Path == "/ws/tools" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"hl7-jira","protocol":"mcp","transport":{"type":"manager_proxy"}}]`)
		case r.URL.Path == "/ws/tools/hl7-jira" && r.Method == http.MethodGet:
			body, _ := io.ReadAll(r.Body)
			getBodies = append(getBodies, string(body))
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"name":"hl7-jira","protocol":"mcp","transport":{"type":"manager_proxy"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	runtimeURL, err := url.Parse(runtime.URL)
	if err != nil {
		t.Fatal(err)
	}

	stateRoot := t.TempDir()
	supportRoot := filepath.Join(stateRoot, "catalog", "hl7-jira-support")
	if err := os.MkdirAll(filepath.Join(supportRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(supportRoot, "bin", "hl7-jira-mcp.js"), []byte("#!/usr/bin/env bun\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(supportRoot, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(supportRoot, "data", "jira-data.db"), []byte("sqlite"), 0o644); err != nil {
		t.Fatal(err)
	}

	launcher := &fakeLauncher{
		baseDir: stateRoot,
		runtime: &Runtime{
			Name:    "bp-ig-fix",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{
		DefaultNamespace: "acme",
		Launcher:         launcher,
		StateRoot:        stateRoot,
		LocalTools: []LocalTool{
			{
				Name:        "hl7-jira-support",
				Exposure:    localToolExposureSupportBundle,
				Description: "Jira MCP support bundle",
				HostRoot:    supportRoot,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetInternalBaseURL("http://127.0.0.1:31337")
	server := httptest.NewServer(mgr)
	defer server.Close()

	createBody := `{"name":"bp-ig-fix","topics":[{"name":"bp-panel-validator"}],"runtime":{"localTools":["hl7-jira-support"]}}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, req, "alice@example.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", res.StatusCode)
	}

	registerReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces/bp-ig-fix/tools", strings.NewReader(`{
		"name":"hl7-jira",
		"description":"Search and inspect issues from the real HL7 Jira SQLite snapshot",
		"protocol":"mcp",
		"provider":"demo@acme.example",
		"credentialRef":"secret://jira/demo",
		"transport":{
			"type":"stdio",
			"command":"bun",
			"args":["/tools/hl7-jira-support/bin/hl7-jira-mcp.js"],
			"cwd":"/tools/hl7-jira-support",
			"env":{"HL7_JIRA_DB":"/tools/hl7-jira-support/data/jira-data.db"}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	registerReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, registerReq, "alice@example.com")
	registerRes, err := http.DefaultClient.Do(registerReq)
	if err != nil {
		t.Fatal(err)
	}
	defer registerRes.Body.Close()
	if registerRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(registerRes.Body)
		t.Fatalf("register status = %d body=%s", registerRes.StatusCode, string(body))
	}
	detailBody, err := io.ReadAll(registerRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(detailBody), `"command":"bun"`) || !strings.Contains(string(detailBody), `"/tools/hl7-jira-support/bin/hl7-jira-mcp.js"`) {
		t.Fatalf("expected public tool detail to preserve original transport, got %s", string(detailBody))
	}
	if !strings.Contains(string(detailBody), `"HL7_JIRA_DB":{"redacted":true}`) {
		t.Fatalf("expected public tool detail to redact env values, got %s", string(detailBody))
	}
	if strings.Contains(string(detailBody), `"/tools/hl7-jira-support/data/jira-data.db"`) {
		t.Fatalf("expected public tool detail to hide env values, got %s", string(detailBody))
	}
	if strings.Contains(string(detailBody), `"type":"manager_proxy"`) {
		t.Fatalf("expected public tool detail to hide manager_proxy, got %s", string(detailBody))
	}
	listReq, err := http.NewRequest(http.MethodGet, server.URL+"/apis/v1/namespaces/acme/workspaces/bp-ig-fix/tools", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, listReq, "alice@example.com")
	listRes, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatal(err)
	}
	defer listRes.Body.Close()
	listBody, err := io.ReadAll(listRes.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(listBody), `"kind":"local"`) || !strings.Contains(string(listBody), `"kind":"mcp"`) {
		t.Fatalf("expected public tool inventory to include local and mcp entries, got %s", string(listBody))
	}
	if !strings.Contains(string(listBody), `"name":"hl7-jira"`) || !strings.Contains(string(listBody), `"name":"hl7-jira-support"`) {
		t.Fatalf("expected public tool inventory to include both enabled tools, got %s", string(listBody))
	}
	if strings.Contains(string(listBody), `"/tools/hl7-jira-support/data/jira-data.db"`) || strings.Contains(string(listBody), `"type":"manager_proxy"`) {
		t.Fatalf("expected public tool inventory to stay minimal, got %s", string(listBody))
	}

	if len(createBodies) != 1 {
		t.Fatalf("expected one runtime tool create, got %d", len(createBodies))
	}
	if !strings.Contains(createBodies[0], `"type":"manager_proxy"`) {
		t.Fatalf("expected runtime tool create to use manager_proxy, got %s", createBodies[0])
	}
	if strings.Contains(createBodies[0], "hl7-jira-support") || strings.Contains(createBodies[0], "secret://jira/demo") {
		t.Fatalf("expected runtime tool create to avoid leaking host transport details, got %s", createBodies[0])
	}
	if len(getBodies) == 0 {
		t.Fatalf("expected manager to fetch runtime detail after create")
	}

	bindingPath := filepath.Join(stateRoot, managerPrivateStateDir, "acme", "bp-ig-fix", "tools", "hl7-jira.json")
	bindingRaw, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatalf("read managed tool binding: %v", err)
	}
	if !strings.Contains(string(bindingRaw), `"command": "bun"`) {
		t.Fatalf("expected managed binding to retain bun transport, got %s", string(bindingRaw))
	}
	if !strings.Contains(string(bindingRaw), supportRoot) || !strings.Contains(string(bindingRaw), `"credentialRef": "secret://jira/demo"`) {
		t.Fatalf("expected managed binding to retain resolved host transport and credential ref, got %s", string(bindingRaw))
	}
}

func TestManagerCompatibilityRoutesRemoved(t *testing.T) {
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: &url.URL{Scheme: "http", Host: "example.invalid"},
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	for _, path := range []string{
		"/workspaces",
		"/workspaces/demo",
		"/acp/acme/demo/topics/general",
	} {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", path, res.StatusCode)
		}
	}
}

func TestManagerShelleyUILinksDisabled(t *testing.T) {
	mgr, err := New(Config{
		DefaultNamespace: "acme",
		Launcher:         &fakeLauncher{baseDir: t.TempDir()},
		ShelleyUIMode:    "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}

	ws := &Workspace{
		Namespace: "acme",
		Name:      "demo",
		Runtime: Runtime{
			APIBase: &url.URL{Scheme: "http", Host: "127.0.0.1:12345"},
		},
	}
	mgr.workspaces[workspaceKey{namespace: "acme", name: "demo"}] = ws

	req := httptest.NewRequest(http.MethodGet, "/apis/v1/namespaces/acme/workspaces/demo", nil)
	req.Host = "agentic-workspace.exe.xyz"
	setManagerAuth(t, req, "alice@example.com")

	detail := mgr.specDetail(req, ws, []string{"general"})
	if len(detail.Topics) != 1 {
		t.Fatalf("expected one topic, got %#v", detail.Topics)
	}
	if detail.Topics[0].Shelley != "" {
		t.Fatalf("expected no Shelley URL in disabled mode, got %#v", detail.Topics[0])
	}

	redirectReq := httptest.NewRequest(http.MethodGet, "/shelley/acme/demo/general", nil)
	redirectReq.Host = "agentic-workspace.exe.xyz"
	redirectRes := httptest.NewRecorder()
	mgr.ServeHTTP(redirectRes, redirectReq)
	if redirectRes.Code != http.StatusNotFound {
		t.Fatalf("expected disabled Shelley route to 404, got %d", redirectRes.Code)
	}
}

func TestManagerShelleyUILinksSameHostPort(t *testing.T) {
	mgr, err := New(Config{
		DefaultNamespace: "acme",
		Launcher:         &fakeLauncher{baseDir: t.TempDir()},
		ShelleyUIMode:    "same_host_port",
	})
	if err != nil {
		t.Fatal(err)
	}

	ws := &Workspace{
		Namespace: "acme",
		Name:      "demo",
		Runtime: Runtime{
			APIBase: &url.URL{Scheme: "http", Host: "127.0.0.1:12345"},
		},
	}
	mgr.workspaces[workspaceKey{namespace: "acme", name: "demo"}] = ws

	req := httptest.NewRequest(http.MethodGet, "/apis/v1/namespaces/acme/workspaces/demo", nil)
	req.Host = "agentic-workspace.exe.xyz"
	req.Header.Set("X-Forwarded-Proto", "https")
	setManagerAuth(t, req, "alice@example.com")

	detail := mgr.specDetail(req, ws, []string{"general"})
	if len(detail.Topics) != 1 {
		t.Fatalf("expected one topic, got %#v", detail.Topics)
	}
	if got, want := detail.Topics[0].Shelley, "https://agentic-workspace.exe.xyz:12345/c/general"; got != want {
		t.Fatalf("Shelley URL = %q, want %q", got, want)
	}

	redirectReq := httptest.NewRequest(http.MethodGet, "/shelley/acme/demo/general", nil)
	redirectReq.Host = "agentic-workspace.exe.xyz"
	redirectReq.Header.Set("X-Forwarded-Proto", "https")
	redirectRes := httptest.NewRecorder()
	mgr.ServeHTTP(redirectRes, redirectReq)
	if redirectRes.Code != http.StatusFound {
		t.Fatalf("expected Shelley redirect, got %d", redirectRes.Code)
	}
	if got, want := redirectRes.Header().Get("Location"), "https://agentic-workspace.exe.xyz:12345/c/general"; got != want {
		t.Fatalf("redirect location = %q, want %q", got, want)
	}
}

func TestManagerCanonicalTopicEventsProxy(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/ws/topics/general/events":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket: %v", err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"connected","topic":"general","protocolVersion":"workspace-topic-v1"}`)); err != nil {
				t.Fatalf("write websocket: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	runtimeURL, _ := url.Parse(runtime.URL)
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/apis/v1/namespaces/acme/workspaces/demo/topics/general/events"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	authenticateManagerWS(t, ctx, conn, "alice@example.com")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"connected"`) {
		t.Fatalf("unexpected websocket payload %s", data)
	}
}

func TestManagerCanonicalNamespaceEvents(t *testing.T) {
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:   "demo",
			Mode:   "fake",
			Health: func(context.Context) error { return nil },
			Stop:   func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/apis/v1/namespaces/acme/events"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	authenticateManagerWS(t, ctx, conn, "alice@example.com")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"protocolVersion":"workspace-manager-v1"`) {
		t.Fatalf("unexpected namespace events payload %s", data)
	}
}

func TestManagerCanonicalTopicEventsProxyForwardsTrustedIdentity(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/ws/topics/general/events":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket: %v", err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			if got := r.Header.Get(workspaceHeaderSubject); got != "alice@example.com" {
				t.Fatalf("expected trusted subject header, got %q", got)
			}
			if got := r.Header.Get(workspaceHeaderDisplayName); got != "alice@example.com" {
				t.Fatalf("expected trusted display-name header, got %q", got)
			}
			if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"connected","topic":"general","protocolVersion":"workspace-topic-v1"}`)); err != nil {
				t.Fatalf("write websocket: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	runtimeURL, _ := url.Parse(runtime.URL)
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/apis/v1/namespaces/acme/workspaces/demo/topics/general/events"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{server.URL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	authenticateManagerWS(t, ctx, conn, "alice@example.com")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"connected"`) {
		t.Fatalf("unexpected websocket payload %s", data)
	}
}

func TestManagerDeleteStopsRuntime(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws/health" {
			io.WriteString(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	stopCount := 0
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop: func(context.Context) error {
				stopCount++
				return nil
			},
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/apis/v1/namespaces/acme/workspaces/demo", nil)
	setManagerAuth(t, req, "alice@example.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", res.StatusCode)
	}
	if stopCount != 1 {
		t.Fatalf("expected one runtime stop, got %d", stopCount)
	}
}

func TestManagerDeleteRemovesWorkspaceState(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws/health" {
			io.WriteString(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	stateRoot := t.TempDir()
	launcher := &fakeLauncher{
		baseDir: stateRoot,
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher, StateRoot: stateRoot})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	workspaceDir := filepath.Join(stateRoot, "acme", "demo")
	if _, err := os.Stat(workspaceDir); err != nil {
		t.Fatalf("expected workspace state dir to exist before delete: %v", err)
	}

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/apis/v1/namespaces/acme/workspaces/demo", nil)
	setManagerAuth(t, req, "alice@example.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", res.StatusCode)
	}
	if _, err := os.Stat(workspaceDir); !os.IsNotExist(err) {
		t.Fatalf("expected workspace state dir to be removed, stat err = %v", err)
	}
}

func TestManagerShutdownStopsTrackedRuntimes(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws/health" {
			io.WriteString(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	stopCount := 0
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop: func(context.Context) error {
				stopCount++
				return nil
			},
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	for _, name := range []string{"demo-a", "demo-b"} {
		createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"`+name+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		createReq.Header.Set("Content-Type", "application/json")
		setManagerAuth(t, createReq, "alice@example.com")
		createRes, err := http.DefaultClient.Do(createReq)
		if err != nil {
			t.Fatal(err)
		}
		createRes.Body.Close()
	}

	if err := mgr.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if stopCount != 2 {
		t.Fatalf("expected two runtime stops, got %d", stopCount)
	}
}

func TestDecodeTopicsObjectForm(t *testing.T) {
	topics, err := decodeTopics(json.RawMessage(`[{"name":"general"},{"name":"debug"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(topics, ","), "general,debug"; got != want {
		t.Fatalf("topics = %q, want %q", got, want)
	}
}

func TestManagerLocalToolsCatalogAndWorkspaceSelection(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"bp-example-validator"}`)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"bp-example-validator","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	baseDir := t.TempDir()
	launcher := &fakeLauncher{
		baseDir: baseDir,
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	localTools := []LocalTool{{
		Name:         "fhir-validator",
		Description:  "FHIR Validator CLI",
		Guidance:     "Run through bash as `fhir-validator`.",
		Requirements: []string{"java"},
		HostRoot:     t.TempDir(),
		Commands:     []LocalToolCommand{{Name: "fhir-validator", RelativePath: "bin/fhir-validator"}},
	}}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher, LocalTools: localTools})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	catalogReq, err := http.NewRequest(http.MethodGet, server.URL+"/apis/v1/local-tools", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, catalogReq, "alice@example.com")
	catalogRes, err := http.DefaultClient.Do(catalogReq)
	if err != nil {
		t.Fatal(err)
	}
	defer catalogRes.Body.Close()
	if catalogRes.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", catalogRes.StatusCode)
	}
	var catalog []localToolInfo
	if err := json.NewDecoder(catalogRes.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 || catalog[0].Name != "fhir-validator" {
		t.Fatalf("unexpected catalog %#v", catalog)
	}
	if len(catalog[0].Requirements) != 1 || catalog[0].Requirements[0] != "java" {
		t.Fatalf("unexpected catalog requirements %#v", catalog[0].Requirements)
	}

	createBody := `{
		"name":"demo",
		"template":"acme-rpm-ig",
		"topics":[{"name":"bp-example-validator"}],
		"runtime":{"localTools":["fhir-validator"]}
	}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, req, "alice@example.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", res.StatusCode)
	}

	var detail workspaceDetail
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Runtime == nil || len(detail.Runtime.LocalTools) != 1 || detail.Runtime.LocalTools[0].Name != "fhir-validator" {
		t.Fatalf("unexpected runtime local tools %#v", detail.Runtime)
	}

	launcher.mu.Lock()
	if len(launcher.launches) != 1 || len(launcher.launches[0].LocalTools) != 1 || launcher.launches[0].LocalTools[0].Name != "fhir-validator" {
		launcher.mu.Unlock()
		t.Fatalf("unexpected launch specs %#v", launcher.launches)
	}
	launchSpec := launcher.launches[0]
	launcher.mu.Unlock()

	guidancePath := filepath.Join(launchSpec.WorkspaceDir, ".shelley", "AGENTS.md")
	content, err := os.ReadFile(guidancePath)
	if err != nil {
		t.Fatalf("failed to read local tool guidance: %v", err)
	}
	if !strings.Contains(string(content), "fhir-validator") {
		t.Fatalf("expected local tool guidance to mention fhir-validator, got %q", string(content))
	}

	seededPatient, err := os.ReadFile(filepath.Join(launchSpec.WorkspaceDir, "input", "examples", "Patient-bp-alice-smith.json"))
	if err != nil {
		t.Fatalf("failed to read seeded demo patient example: %v", err)
	}
	if !strings.Contains(string(seededPatient), `"gender": "woman"`) {
		t.Fatalf("expected seeded demo patient content, got %q", string(seededPatient))
	}
	seededObservation, err := os.ReadFile(filepath.Join(launchSpec.WorkspaceDir, "input", "examples", "Observation-bp-alice-morning.json"))
	if err != nil {
		t.Fatalf("failed to read seeded demo observation example: %v", err)
	}
	if !strings.Contains(string(seededObservation), `"effectiveDateTime": "2026-02-30T07:00:00Z"`) {
		t.Fatalf("expected seeded demo observation content, got %q", string(seededObservation))
	}
}

func TestManagerRecoverPersistedWorkspaces(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"bp-example-validator"}`)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"bp-example-validator","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	stateRoot := t.TempDir()
	localTools := []LocalTool{{
		Name:         "fhir-validator",
		Description:  "FHIR Validator CLI",
		Guidance:     "Run through bash as `fhir-validator`.",
		Requirements: []string{"java"},
		HostRoot:     t.TempDir(),
		Commands:     []LocalToolCommand{{Name: "fhir-validator", RelativePath: "bin/fhir-validator"}},
	}}

	launcherA := &fakeLauncher{
		baseDir: stateRoot,
		runtime: &Runtime{
			Name:    "bp-ig-fix",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgrA, err := New(Config{DefaultNamespace: "acme", Launcher: launcherA, LocalTools: localTools, StateRoot: stateRoot})
	if err != nil {
		t.Fatal(err)
	}
	serverA := httptest.NewServer(mgrA)

	createBody := `{"name":"bp-ig-fix","template":"acme-rpm-ig","topics":[{"name":"bp-example-validator"}],"runtime":{"localTools":["fhir-validator"]}}`
	createReq, err := http.NewRequest(http.MethodPost, serverA.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.StatusCode)
	}
	if err := mgrA.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	serverA.Close()

	launcherB := &fakeLauncher{
		baseDir: stateRoot,
		runtime: &Runtime{
			Name:    "bp-ig-fix",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgrB, err := New(Config{DefaultNamespace: "acme", Launcher: launcherB, LocalTools: localTools, StateRoot: stateRoot})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := mgrB.RecoverWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("expected one recovered workspace, got %d", recovered)
	}

	serverB := httptest.NewServer(mgrB)
	defer serverB.Close()

	detailReq, err := http.NewRequest(http.MethodGet, serverB.URL+"/apis/v1/namespaces/acme/workspaces/bp-ig-fix", nil)
	if err != nil {
		t.Fatal(err)
	}
	setManagerAuth(t, detailReq, "alice@example.com")
	detailRes, err := http.DefaultClient.Do(detailReq)
	if err != nil {
		t.Fatal(err)
	}
	defer detailRes.Body.Close()
	if detailRes.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d", detailRes.StatusCode)
	}
	var detail workspaceDetail
	if err := json.NewDecoder(detailRes.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Runtime == nil || len(detail.Runtime.LocalTools) != 1 || detail.Runtime.LocalTools[0].Name != "fhir-validator" {
		t.Fatalf("unexpected recovered runtime local tools %#v", detail.Runtime)
	}
	if detail.Name != "bp-ig-fix" {
		t.Fatalf("unexpected recovered workspace detail %#v", detail)
	}

	metadataPath := filepath.Join(stateRoot, "acme", "bp-ig-fix", workspaceMetadataFilename)
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"template": "acme-rpm-ig"`) {
		t.Fatalf("expected metadata to preserve template, got %s", raw)
	}
}

func TestManagerPatchWorkspaceLocalTools(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"bp-example-validator"}`)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"bp-example-validator","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	stopCount := 0
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "bp-ig-fix",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop: func(context.Context) error {
				stopCount++
				return nil
			},
		},
	}
	localTools := []LocalTool{
		{
			Name:        "fhir-validator",
			Description: "FHIR Validator CLI",
			HostRoot:    t.TempDir(),
			Commands:    []LocalToolCommand{{Name: "fhir-validator", RelativePath: "bin/fhir-validator"}},
		},
		{
			Name:        "hl7-jira-support",
			Exposure:    localToolExposureSupportBundle,
			Description: "HL7 Jira support bundle",
			HostRoot:    t.TempDir(),
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher, LocalTools: localTools})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"bp-ig-fix","topics":[{"name":"bp-example-validator"}],"runtime":{"localTools":["fhir-validator"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.StatusCode)
	}

	req, err := http.NewRequest(http.MethodPatch, server.URL+"/apis/v1/namespaces/acme/workspaces/bp-ig-fix", strings.NewReader(`{"runtime":{"localTools":["fhir-validator","hl7-jira-support"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, req, "alice@example.com")
	patchRes, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer patchRes.Body.Close()
	if patchRes.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", patchRes.StatusCode)
	}
	var detail workspaceDetail
	if err := json.NewDecoder(patchRes.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.Runtime == nil || len(detail.Runtime.LocalTools) != 2 {
		t.Fatalf("unexpected patched runtime local tools %#v", detail.Runtime)
	}
	if stopCount != 1 {
		t.Fatalf("expected one runtime stop during patch, got %d", stopCount)
	}

	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	if len(launcher.launches) != 2 {
		t.Fatalf("expected create + relaunch after patch, got %d launches", len(launcher.launches))
	}
	last := launcher.launches[len(launcher.launches)-1]
	if !sameLocalToolSelection(last.LocalTools, localTools) {
		t.Fatalf("unexpected launch local tools %#v", last.LocalTools)
	}
}

func TestManagerHealthReportsManagerAndShelleyVersions(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/version":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"version":"0.10.0","tag":"v0.10.0","commit":"abc123","commit_time":"2026-03-11T20:00:00Z","modified":false}`)
		case "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.StatusCode)
	}

	res, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", res.StatusCode)
	}

	var health struct {
		Status          string               `json:"status"`
		Manager         buildInfo            `json:"manager"`
		ShelleyRuntimes []runtimeVersionInfo `json:"shelleyRuntimes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}

	if health.Status != "ok" {
		t.Fatalf("unexpected health status %q", health.Status)
	}
	if health.Manager.Name != "shelleymanager" {
		t.Fatalf("unexpected manager health block %+v", health.Manager)
	}
	if len(health.ShelleyRuntimes) != 1 {
		t.Fatalf("expected one Shelley runtime, got %+v", health.ShelleyRuntimes)
	}
	runtimeHealth := health.ShelleyRuntimes[0]
	if runtimeHealth.Namespace != "acme" || runtimeHealth.Workspace != "demo" {
		t.Fatalf("unexpected Shelley runtime block %+v", runtimeHealth)
	}
	if runtimeHealth.Version == nil {
		t.Fatalf("expected Shelley version block, got %+v", runtimeHealth)
	}
	if runtimeHealth.Version.Name != "shelley" || runtimeHealth.Version.Commit != "abc123" {
		t.Fatalf("unexpected Shelley version %+v", runtimeHealth.Version)
	}
	if runtimeHealth.VersionError != "" {
		t.Fatalf("unexpected Shelley version error %q", runtimeHealth.VersionError)
	}
}

func TestManagerRecoverWorkspacesIgnoresOtherNamespaces(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws/health" {
			io.WriteString(w, `{"status":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	stateRoot := t.TempDir()
	otherStateDir := filepath.Join(stateRoot, "other", "ignored")
	if err := os.MkdirAll(filepath.Join(otherStateDir, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherStateDir, workspaceMetadataFilename), []byte(`{"namespace":"other","name":"ignored"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	launcher := &fakeLauncher{
		baseDir: stateRoot,
		runtime: &Runtime{
			Name:    "ignored",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher, StateRoot: stateRoot})
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := mgr.RecoverWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 0 {
		t.Fatalf("expected no recovered workspaces outside default namespace, got %d", recovered)
	}
	if len(launcher.launches) != 0 {
		t.Fatalf("expected no runtime launches for ignored namespaces, got %d", len(launcher.launches))
	}
}

func TestManagerRejectsNonDefaultNamespaceRoutes(t *testing.T) {
	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "demo",
			APIBase: &url.URL{Scheme: "http", Host: "example.invalid"},
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{DefaultNamespace: "acme", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	for _, path := range []string{
		"/apis/v1/namespaces/other/workspaces",
		"/apis/v1/namespaces/other/workspaces/demo",
		"/apis/v1/namespaces/other/events",
	} {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		setManagerAuth(t, req, "alice@example.com")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", path, res.StatusCode)
		}
	}
}

func TestManagerUIRoutes(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"bp-example-validator"}`)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"bp-example-validator","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	launcher := &fakeLauncher{
		baseDir: t.TempDir(),
		runtime: &Runtime{
			Name:    "bp-ig-fix",
			APIBase: runtimeURL,
			Mode:    "fake",
			Health:  func(context.Context) error { return nil },
			Stop:    func(context.Context) error { return nil },
		},
	}
	mgr, err := New(Config{
		DefaultNamespace: "acme",
		Launcher:         launcher,
		LocalTools: []LocalTool{{
			Name:        "fhir-validator",
			Description: "FHIR Validator CLI",
			HostRoot:    t.TempDir(),
			Commands:    []LocalToolCommand{{Name: "fhir-validator", RelativePath: "bin/fhir-validator"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mgr)
	defer server.Close()

	homeRes, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer homeRes.Body.Close()
	homeBody, _ := io.ReadAll(homeRes.Body)
	if !strings.Contains(string(homeBody), `<div id="root"></div>`) || !strings.Contains(string(homeBody), `/assets/`) {
		t.Fatalf("unexpected home page body: %s", homeBody)
	}
	if !strings.Contains(string(homeBody), "<title>Shelley Manager</title>") {
		t.Fatalf("expected home page title in body, got %s", homeBody)
	}

	guideRes, err := http.Get(server.URL + "/about")
	if err != nil {
		t.Fatal(err)
	}
	defer guideRes.Body.Close()
	guideBody, _ := io.ReadAll(guideRes.Body)
	if !strings.Contains(string(guideBody), `<div id="root"></div>`) || !strings.Contains(string(guideBody), `/assets/`) {
		t.Fatalf("unexpected about page body: %s", guideBody)
	}

	createReq, err := http.NewRequest(http.MethodPost, server.URL+"/apis/v1/namespaces/acme/workspaces", strings.NewReader(`{"name":"bp-ig-fix","topics":[{"name":"bp-example-validator"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	createReq.Header.Set("Content-Type", "application/json")
	setManagerAuth(t, createReq, "alice@example.com")
	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.StatusCode)
	}

	appRes, err := http.Get(server.URL + "/app/acme/bp-ig-fix/bp-example-validator")
	if err != nil {
		t.Fatal(err)
	}
	defer appRes.Body.Close()
	appBody, _ := io.ReadAll(appRes.Body)
	if !strings.Contains(string(appBody), `<div id="root"></div>`) || !strings.Contains(string(appBody), `/assets/`) {
		t.Fatalf("unexpected app page body: %s", appBody)
	}
}
