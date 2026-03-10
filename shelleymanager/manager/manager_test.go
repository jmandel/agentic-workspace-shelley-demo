package manager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
}

func (f *fakeLauncher) Name() string { return "fake" }

func (f *fakeLauncher) WorkspacePaths(namespace, name string) (LaunchSpec, error) {
	return LaunchSpec{
		Namespace:    namespace,
		Name:         name,
		StateDir:     "/tmp/" + namespace + "/" + name,
		WorkspaceDir: "/tmp/" + namespace + "/" + name + "/workspace",
		DBPath:       "/tmp/" + namespace + "/" + name + "/shelley.db",
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

	mu.Lock()
	got := strings.Join(seenPaths, "\n")
	mu.Unlock()
	for _, expected := range []string{
		"POST /ws/topics",
		"GET /ws/topics",
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
