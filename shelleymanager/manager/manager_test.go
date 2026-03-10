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
		base = f.baseDir
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
	if f.runtime != nil && f.runtime.Stop != nil {
		origStop := f.runtime.Stop
		f.runtime.Stop = func(ctx context.Context) error {
			f.mu.Lock()
			f.stops++
			f.mu.Unlock()
			return origStop(ctx)
		}
	}
	return f.runtime, nil
}

func TestManagerCreateAndProxyRoutes(t *testing.T) {
	var seenPaths []string
	var mu sync.Mutex

	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenPaths = append(seenPaths, r.Method+" "+r.URL.Path)
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
			io.WriteString(w, `[{"name":"general","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		case strings.HasPrefix(r.URL.Path, "/ws/files/"):
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
	res, err := http.Post(server.URL+"/apis/v1/namespaces/acme/workspaces", "application/json", strings.NewReader(createBody))
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
	if created.Name != "demo" || created.API == "" || created.Endpoint == "" {
		t.Fatalf("unexpected create response: %+v", created)
	}

	topicsRes, err := http.Get(server.URL + "/apis/v1/namespaces/acme/workspaces/demo/topics")
	if err != nil {
		t.Fatal(err)
	}
	defer topicsRes.Body.Close()
	body, _ := io.ReadAll(topicsRes.Body)
	if !strings.Contains(string(body), "general") {
		t.Fatalf("expected proxied topics response, got %s", body)
	}

	fileRes, err := http.Get(server.URL + "/workspaces/demo/files/readme.txt")
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
		"GET /ws/files/readme.txt",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected runtime to see %q, got:\n%s", expected, got)
		}
	}
}

func TestManagerCompatibilityRoutes(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"general","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	launcher := &fakeLauncher{
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

	createRes, err := http.Post(server.URL+"/workspaces", "application/json", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.StatusCode)
	}

	detailRes, err := http.Get(server.URL + "/workspaces/demo")
	if err != nil {
		t.Fatal(err)
	}
	defer detailRes.Body.Close()
	var detail map[string]any
	if err := json.NewDecoder(detailRes.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail["api"] == "" || detail["acp"] == "" {
		t.Fatalf("missing compatibility discovery fields: %#v", detail)
	}
}

func TestManagerACPProxy(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/ws/topic/general":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket: %v", err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"connected","topic":"general","sessionId":"s1"}`)); err != nil {
				t.Fatalf("write websocket: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	runtimeURL, _ := url.Parse(runtime.URL)
	launcher := &fakeLauncher{
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

	createRes, err := http.Post(server.URL+"/workspaces", "application/json", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/acp/acme/demo/topics/general"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"connected"`) {
		t.Fatalf("unexpected websocket payload %s", data)
	}
}

func TestManagerACPProxyRewritesBrowserOrigin(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case "/ws/topic/general":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket with browser origin: %v", err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			if got := r.Header.Get("X-Forwarded-Origin"); got == "" {
				t.Fatal("expected proxy to preserve original browser origin")
			}
			if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"connected","topic":"general","sessionId":"s1"}`)); err != nil {
				t.Fatalf("write websocket: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()

	runtimeURL, _ := url.Parse(runtime.URL)
	launcher := &fakeLauncher{
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

	createRes, err := http.Post(server.URL+"/workspaces", "application/json", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/acp/acme/demo/topics/general"
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

	createRes, err := http.Post(server.URL+"/workspaces", "application/json", strings.NewReader(`{"name":"demo"}`))
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, server.URL+"/workspaces/demo", nil)
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
			io.WriteString(w, `{"name":"bp-panel-validator"}`)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"bp-panel-validator","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
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

	catalogRes, err := http.Get(server.URL + "/apis/v1/local-tools")
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
		"topics":[{"name":"bp-panel-validator"}],
		"runtime":{"localTools":["fhir-validator"]}
	}`
	res, err := http.Post(server.URL+"/apis/v1/namespaces/acme/workspaces", "application/json", strings.NewReader(createBody))
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

	seededProfile, err := os.ReadFile(filepath.Join(launchSpec.WorkspaceDir, "input", "fsh", "BloodPressurePanel.fsh"))
	if err != nil {
		t.Fatalf("failed to read seeded demo template profile: %v", err)
	}
	if !strings.Contains(string(seededProfile), "component contains") {
		t.Fatalf("expected seeded demo profile content, got %q", string(seededProfile))
	}
}

func TestManagerUIRoutes(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ws/health":
			io.WriteString(w, `{"status":"ok"}`)
		case r.URL.Path == "/ws/topics" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"name":"bp-panel-validator"}`)
		case r.URL.Path == "/ws/topics":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"name":"bp-panel-validator","clients":0,"busy":false,"createdAt":"2026-03-10T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	launcher := &fakeLauncher{
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
	if !strings.Contains(string(homeBody), "Create Workspace") || !strings.Contains(string(homeBody), "/apis/v1/local-tools") {
		t.Fatalf("unexpected home page body: %s", homeBody)
	}

	createRes, err := http.Post(server.URL+"/apis/v1/namespaces/acme/workspaces", "application/json", strings.NewReader(`{"name":"bp-ig-fix","topics":[{"name":"bp-panel-validator"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	createRes.Body.Close()
	if createRes.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.StatusCode)
	}

	appRes, err := http.Get(server.URL + "/app/acme/bp-ig-fix/bp-panel-validator")
	if err != nil {
		t.Fatal(err)
	}
	defer appRes.Body.Close()
	appBody, _ := io.ReadAll(appRes.Body)
	if !strings.Contains(string(appBody), "WS_MANAGER") || !strings.Contains(string(appBody), "/acp/acme/bp-ig-fix/topics/bp-panel-validator") {
		t.Fatalf("unexpected app page body: %s", appBody)
	}
	if !strings.Contains(string(appBody), "Connecting...") || !strings.Contains(string(appBody), "Realtime connection failed") {
		t.Fatalf("expected app page to expose realtime connection status text, got %s", appBody)
	}
	if !strings.Contains(string(appBody), "Delete Topic") {
		t.Fatalf("expected app page body to expose topic deletion controls, got %s", appBody)
	}
	if !strings.Contains(string(appBody), "Prompt Queue") || !strings.Contains(string(appBody), "Clear My Queue") || !strings.Contains(string(appBody), "Refresh Queue") {
		t.Fatalf("expected app page body to expose queue controls, got %s", appBody)
	}
}
