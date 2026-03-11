package manager

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProcessCommandSetsWorkspaceToolsEnv(t *testing.T) {
	launcher := CommandLauncher{
		Mode:          "process",
		StateRoot:     "/tmp/state",
		ShelleyBinary: "/usr/local/bin/shelley",
	}
	spec, err := launcher.WorkspacePaths("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := launcher.buildProcessCommand(spec, 43123)
	if err != nil {
		t.Fatal(err)
	}

	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "WORKSPACE_NAME=demo") {
		t.Fatalf("expected workspace name env, got %q", env)
	}
	if !strings.Contains(env, "WORKSPACE_TOOLS_DIR="+filepath.Join(spec.StateDir, "tools")) {
		t.Fatalf("expected tools env, got %q", env)
	}
	if !strings.Contains(env, "HOME="+filepath.Join(spec.StateDir, "home")) {
		t.Fatalf("expected workspace-local home env, got %q", env)
	}
	if !strings.Contains(env, "JAVA_TOOL_OPTIONS=-Duser.home="+filepath.Join(spec.StateDir, "home")) {
		t.Fatalf("expected java user.home override, got %q", env)
	}
	if !strings.Contains(env, "PATH="+filepath.Join(spec.StateDir, "tools", "bin")) {
		t.Fatalf("expected tools bin in path, got %q", env)
	}
}

func TestBuildDockerCommand(t *testing.T) {
	launcher := CommandLauncher{
		Mode:            "docker",
		StateRoot:       "/tmp/state",
		DockerBinary:    "docker",
		DockerImage:     "example/shelley:latest",
		DockerCommand:   "shelley",
		DefaultModel:    "predictable",
		PredictableOnly: true,
	}
	spec, err := launcher.WorkspacePaths("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := launcher.buildDockerCommand(spec, 43123)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(cmd.Path) != "docker" {
		t.Fatalf("docker binary = %q", cmd.Path)
	}
	got := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"run --rm --init",
		"--name shelley-acme-demo",
		"-p 127.0.0.1:43123:43123",
		"-v " + spec.WorkspaceDir + ":/workspace",
		"-v " + filepath.Join(spec.StateDir, "tools") + ":/tools",
		"-e HOME=/state/home",
		"-e JAVA_TOOL_OPTIONS=-Duser.home=/state/home",
		"-e WORKSPACE_TOOLS_DIR=/tools",
		"example/shelley:latest shelley",
		"-db /state/shelley.db",
		"serve -port 43123 -workspace-dir /workspace -socket none",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected docker args to contain %q, got %q", want, got)
		}
	}
}

func TestBuildDockerCommandMountsSelectedLocalToolRoots(t *testing.T) {
	toolRoot := t.TempDir()
	launcher := CommandLauncher{
		Mode:         "docker",
		StateRoot:    "/tmp/state",
		DockerBinary: "docker",
		DockerImage:  "example/shelley:latest",
	}
	spec, err := launcher.WorkspacePaths("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}
	spec.LocalTools = []LocalTool{{
		Name:     "fhir-validator",
		HostRoot: toolRoot,
		Commands: []LocalToolCommand{{Name: "fhir-validator", RelativePath: "bin/fhir-validator"}},
	}}

	cmd, err := launcher.buildDockerCommand(spec, 43123)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cmd.Args, " ")
	if !strings.Contains(got, "-v "+toolRoot+":/tools/fhir-validator:ro") {
		t.Fatalf("expected selected local tool mount, got %q", got)
	}
}

func TestBuildBwrapCommand(t *testing.T) {
	launcher := CommandLauncher{
		Mode:            "bwrap",
		StateRoot:       "/tmp/state",
		ShelleyBinary:   "/usr/local/bin/shelley",
		BwrapBinary:     "bwrap",
		DefaultModel:    "predictable",
		PredictableOnly: true,
	}
	spec, err := launcher.WorkspacePaths("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}

	cmd, err := launcher.buildBwrapCommand(spec, 43123)
	if err != nil {
		t.Fatal(err)
	}

	if filepath.Base(cmd.Path) != "bwrap" {
		t.Fatalf("bwrap binary = %q", cmd.Path)
	}
	got := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"--die-with-parent",
		"--ro-bind /usr /usr",
		"--bind " + spec.StateDir + " /sandbox",
		"--bind " + filepath.Join(spec.StateDir, "tools") + " /tools",
		"--bind " + filepath.Join(spec.StateDir, "tmp") + " /tmp",
		"--share-net",
		"--setenv WORKSPACE_NAME demo",
		"--setenv HOME /sandbox/home",
		"--setenv JAVA_TOOL_OPTIONS -Duser.home=/sandbox/home",
		"--setenv WORKSPACE_TOOLS_DIR /tools",
		"-- /sandbox/bin/shelley",
		"-db /sandbox/shelley.db",
		"serve -port 43123 -workspace-dir /sandbox/workspace -socket none",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected bwrap args to contain %q, got %q", want, got)
		}
	}
}

func TestBuildBwrapCommandMountsSelectedLocalToolRoots(t *testing.T) {
	toolRoot := t.TempDir()
	launcher := CommandLauncher{
		Mode:          "bwrap",
		StateRoot:     "/tmp/state",
		ShelleyBinary: "/usr/local/bin/shelley",
		BwrapBinary:   "bwrap",
	}
	spec, err := launcher.WorkspacePaths("acme", "demo")
	if err != nil {
		t.Fatal(err)
	}
	spec.LocalTools = []LocalTool{{
		Name:     "fhir-validator",
		HostRoot: toolRoot,
		Commands: []LocalToolCommand{{Name: "fhir-validator", RelativePath: "bin/fhir-validator"}},
	}}

	cmd, err := launcher.buildBwrapCommand(spec, 43123)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cmd.Args, " ")
	if !strings.Contains(got, "--ro-bind "+toolRoot+" /tools/fhir-validator") {
		t.Fatalf("expected selected local tool ro-bind, got %q", got)
	}
}

func TestParseRuntimePortRange(t *testing.T) {
	tests := []struct {
		raw      string
		wantMin  int
		wantMax  int
		wantUsed bool
		wantErr  bool
	}{
		{raw: "", wantUsed: false},
		{raw: "8100", wantMin: 8100, wantMax: 8100, wantUsed: true},
		{raw: "8100-9000", wantMin: 8100, wantMax: 9000, wantUsed: true},
		{raw: " 8100 - 9000 ", wantMin: 8100, wantMax: 9000, wantUsed: true},
		{raw: "9000-8100", wantErr: true},
		{raw: "0-1", wantErr: true},
		{raw: "abc", wantErr: true},
		{raw: "8100-8200-8300", wantErr: true},
	}

	for _, tt := range tests {
		gotMin, gotMax, gotUsed, err := parseRuntimePortRange(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseRuntimePortRange(%q) expected error", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseRuntimePortRange(%q) unexpected error: %v", tt.raw, err)
		}
		if gotMin != tt.wantMin || gotMax != tt.wantMax || gotUsed != tt.wantUsed {
			t.Fatalf("parseRuntimePortRange(%q) = (%d, %d, %v), want (%d, %d, %v)", tt.raw, gotMin, gotMax, gotUsed, tt.wantMin, tt.wantMax, tt.wantUsed)
		}
	}
}

func TestReserveLocalPortInRangeSinglePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	got, err := reserveLocalPortInRange(port, port)
	if err != nil {
		t.Fatalf("reserveLocalPortInRange(%d,%d) unexpected error: %v", port, port, err)
	}
	if got != port {
		t.Fatalf("reserveLocalPortInRange(%d,%d) = %d", port, port, got)
	}
}

func TestReserveLocalPortInRangeFailsWhenOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	if _, err := reserveLocalPortInRange(port, port); err == nil {
		t.Fatalf("expected reserveLocalPortInRange(%d,%d) to fail while occupied", port, port)
	}
}
