package manager

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProcessCommandSetsWorkspaceToolsEnv(t *testing.T) {
	toolsDir := t.TempDir()
	launcher := CommandLauncher{
		Mode:           "process",
		StateRoot:      "/tmp/state",
		ShelleyBinary:  "/usr/local/bin/shelley",
		SharedToolsDir: toolsDir,
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
	if !strings.Contains(env, "WORKSPACE_TOOLS_DIR="+toolsDir) {
		t.Fatalf("expected tools env, got %q", env)
	}
}

func TestBuildDockerCommand(t *testing.T) {
	toolsDir := t.TempDir()
	launcher := CommandLauncher{
		Mode:            "docker",
		StateRoot:       "/tmp/state",
		SharedToolsDir:  toolsDir,
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
		"-v " + toolsDir + ":/tools:ro",
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

func TestBuildBwrapCommand(t *testing.T) {
	toolsDir := t.TempDir()
	launcher := CommandLauncher{
		Mode:            "bwrap",
		StateRoot:       "/tmp/state",
		ShelleyBinary:   "/usr/local/bin/shelley",
		SharedToolsDir:  toolsDir,
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
		"--bind " + filepath.Join(spec.StateDir, "tmp") + " /tmp",
		"--ro-bind " + toolsDir + " /tools",
		"--share-net",
		"--setenv WORKSPACE_NAME demo",
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
