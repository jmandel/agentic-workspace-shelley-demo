package manager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	bwrapSandboxRoot      = "/sandbox"
	bwrapSandboxBinary    = "/sandbox/bin/shelley"
	bwrapSandboxConfig    = "/sandbox/config/shelley.json"
	bwrapSandboxWorkspace = "/sandbox/workspace"
	bwrapSandboxDB        = "/sandbox/shelley.db"
	bwrapSandboxHome      = "/sandbox/home"
	runtimeSharedToolsDir = "/tools"
)

type CommandLauncher struct {
	Mode            string
	StateRoot       string
	ShelleyBinary   string
	SharedToolsDir  string
	DockerBinary    string
	DockerImage     string
	DockerCommand   string
	BwrapBinary     string
	DefaultModel    string
	PredictableOnly bool
	ConfigPath      string
	DebugRuntime    bool
	HealthTimeout   time.Duration
}

func (l CommandLauncher) Name() string {
	mode := strings.TrimSpace(l.Mode)
	if mode == "" {
		mode = "process"
	}
	return mode
}

func (l CommandLauncher) Launch(ctx context.Context, spec LaunchSpec) (*Runtime, error) {
	if err := validateName(spec.Namespace); err != nil {
		return nil, err
	}
	if err := validateName(spec.Name); err != nil {
		return nil, err
	}
	if l.StateRoot == "" {
		return nil, errors.New("state root required")
	}
	mode := strings.TrimSpace(l.Mode)
	if mode == "" {
		mode = "process"
	}
	healthTimeout := l.HealthTimeout
	if healthTimeout <= 0 {
		healthTimeout = 20 * time.Second
	}

	hostPort, err := reserveLocalPort()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(spec.WorkspaceDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(spec.StateDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(spec.StateDir, "tmp"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(spec.StateDir, "home"), 0o755); err != nil {
		return nil, err
	}
	if err := l.prepareWorkspaceTools(spec, mode); err != nil {
		return nil, err
	}
	if mode == "bwrap" {
		if err := l.prepareBwrapState(spec); err != nil {
			return nil, err
		}
	}

	logFilePath := filepath.Join(spec.StateDir, "runtime.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	cmd, err := l.buildCommand(mode, spec, hostPort)
	if err != nil {
		logFile.Close()
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
		logFile.Close()
	}()

	apiBase, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", hostPort))
	healthFn := func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase.String()+"/ws/health", nil)
		if err != nil {
			return err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("health returned %d", res.StatusCode)
		}
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	if err := waitForHealth(waitCtx, healthFn, done); err != nil {
		_ = stopCommand(context.Background(), cmd, done)
		return nil, err
	}

	return &Runtime{
		Name:    spec.Name,
		APIBase: apiBase,
		Mode:    mode,
		Stop: func(ctx context.Context) error {
			return stopCommand(ctx, cmd, done)
		},
		Health: healthFn,
	}, nil
}

func (l CommandLauncher) buildCommand(mode string, spec LaunchSpec, hostPort int) (*exec.Cmd, error) {
	switch mode {
	case "process":
		return l.buildProcessCommand(spec, hostPort)
	case "docker":
		return l.buildDockerCommand(spec, hostPort)
	case "bwrap":
		return l.buildBwrapCommand(spec, hostPort)
	default:
		return nil, fmt.Errorf("unsupported runtime mode %q", mode)
	}
}

func (l CommandLauncher) buildProcessCommand(spec LaunchSpec, hostPort int) (*exec.Cmd, error) {
	if strings.TrimSpace(l.ShelleyBinary) == "" {
		return nil, errors.New("shelley binary required for process mode")
	}
	args := l.shelleyArgs(spec.DBPath, spec.WorkspaceDir, hostPort, l.ConfigPath)
	cmd := exec.Command(l.ShelleyBinary, args...)
	cmd.Env = l.runtimeEnv(os.Environ(), spec, false)
	return cmd, nil
}

func (l CommandLauncher) buildDockerCommand(spec LaunchSpec, hostPort int) (*exec.Cmd, error) {
	dockerBin := strings.TrimSpace(l.DockerBinary)
	if dockerBin == "" {
		dockerBin = "docker"
	}
	image := strings.TrimSpace(l.DockerImage)
	if image == "" {
		return nil, errors.New("docker image required for docker mode")
	}
	entrypoint := strings.TrimSpace(l.DockerCommand)
	if entrypoint == "" {
		entrypoint = "shelley"
	}
	containerState := "/state"
	containerWorkspace := "/workspace"
	containerName := "shelley-" + spec.Namespace + "-" + spec.Name
	args := []string{
		"run", "--rm", "--init",
		"--name", containerName,
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", hostPort, hostPort),
		"-e", "WORKSPACE_NAME=" + spec.Name,
		"-e", "PATH=" + prependPath(filepath.ToSlash(filepath.Join(runtimeSharedToolsDir, "bin")), os.Getenv("PATH")),
		"-v", spec.StateDir + ":" + containerState,
		"-v", spec.WorkspaceDir + ":" + containerWorkspace,
		"-v", filepath.Join(spec.StateDir, "tools") + ":" + runtimeSharedToolsDir,
		"-w", containerWorkspace,
	}
	args = append(args, "-e", "WORKSPACE_TOOLS_DIR="+runtimeSharedToolsDir)
	for _, tool := range spec.LocalTools {
		args = append(args, "-v", tool.HostRoot+":"+filepath.ToSlash(filepath.Join(runtimeSharedToolsDir, tool.Name))+":ro")
	}
	args = append(args, image)
	if entrypoint != "" {
		args = append(args, entrypoint)
	}
	args = append(args, l.shelleyArgs(filepath.Join(containerState, "shelley.db"), containerWorkspace, hostPort, l.ConfigPath)...)
	return exec.Command(dockerBin, args...), nil
}

func (l CommandLauncher) buildBwrapCommand(spec LaunchSpec, hostPort int) (*exec.Cmd, error) {
	if strings.TrimSpace(l.ShelleyBinary) == "" {
		return nil, errors.New("shelley binary required for bwrap mode")
	}
	bwrap := strings.TrimSpace(l.BwrapBinary)
	if bwrap == "" {
		bwrap = "bwrap"
	}
	args := []string{
		"--die-with-parent",
		"--unshare-pid",
		"--unshare-uts",
		"--unshare-ipc",
		"--share-net",
	}
	args = append(args, l.bwrapRuntimeFSArgs(spec)...)
	args = append(args,
		"--bind", spec.StateDir, bwrapSandboxRoot,
		"--bind", filepath.Join(spec.StateDir, "tools"), runtimeSharedToolsDir,
		"--bind", filepath.Join(spec.StateDir, "tmp"), "/tmp",
		"--dev", "/dev",
		"--proc", "/proc",
		"--chdir", bwrapSandboxWorkspace,
		"--setenv", "WORKSPACE_NAME", spec.Name,
		"--setenv", "HOME", bwrapSandboxHome,
		"--setenv", "TMPDIR", "/tmp",
		"--setenv", "WORKSPACE_TOOLS_DIR", runtimeSharedToolsDir,
		"--setenv", "PATH", prependPath(filepath.ToSlash(filepath.Join(runtimeSharedToolsDir, "bin")), os.Getenv("PATH")),
	)
	for _, tool := range spec.LocalTools {
		args = append(args, "--ro-bind", tool.HostRoot, filepath.ToSlash(filepath.Join(runtimeSharedToolsDir, tool.Name)))
	}
	args = append(args, "--", bwrapSandboxBinary)
	args = append(args, l.shelleyArgs(bwrapSandboxDB, bwrapSandboxWorkspace, hostPort, l.bwrapConfigPath(spec))...)
	return exec.Command(bwrap, args...), nil
}

func (l CommandLauncher) shelleyArgs(dbPath, workspaceDir string, hostPort int, configPath string) []string {
	args := make([]string, 0, 16)
	if l.DebugRuntime {
		args = append(args, "-debug")
	}
	if configPath != "" {
		args = append(args, "-config", configPath)
	}
	if l.PredictableOnly {
		args = append(args, "-predictable-only")
	}
	if l.DefaultModel != "" {
		args = append(args, "-default-model", l.DefaultModel)
	}
	args = append(args,
		"-db", dbPath,
		"serve",
		"-port", strconv.Itoa(hostPort),
		"-workspace-dir", workspaceDir,
		"-socket", "none",
	)
	return args
}

func reserveLocalPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForHealth(ctx context.Context, health func(context.Context) error, processDone <-chan error) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-processDone:
			if err == nil {
				return errors.New("runtime exited before becoming healthy")
			}
			return fmt.Errorf("runtime exited before becoming healthy: %w", err)
		case <-ticker.C:
		}
	}
}

func stopCommand(ctx context.Context, cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	select {
	case err := <-done:
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	default:
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	select {
	case err := <-done:
		if err == nil || errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		select {
		case err := <-done:
			if err == nil || errors.Is(err, os.ErrProcessDone) {
				return ctx.Err()
			}
			return errors.Join(ctx.Err(), err)
		default:
			return ctx.Err()
		}
	case <-timer.C:
		_ = cmd.Process.Kill()
		if err := <-done; err == nil || errors.Is(err, os.ErrProcessDone) {
			return nil
		} else {
			return err
		}
	}
}

func (l CommandLauncher) WorkspacePaths(namespace, name string) (LaunchSpec, error) {
	if strings.TrimSpace(l.StateRoot) == "" {
		return LaunchSpec{}, errors.New("state root required")
	}
	base := filepath.Join(l.StateRoot, namespace, name)
	return LaunchSpec{
		Namespace:    namespace,
		Name:         name,
		StateDir:     base,
		WorkspaceDir: filepath.Join(base, "workspace"),
		DBPath:       filepath.Join(base, "shelley.db"),
	}, nil
}

func (l CommandLauncher) prepareWorkspaceTools(spec LaunchSpec, mode string) error {
	toolsRoot := filepath.Join(spec.StateDir, "tools")
	binDir := filepath.Join(toolsRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if len(spec.LocalTools) == 0 {
		return nil
	}

	for _, tool := range spec.LocalTools {
		toolRoot := filepath.Join(toolsRoot, tool.Name)
		switch mode {
		case "process":
			_ = os.RemoveAll(toolRoot)
			if err := os.Symlink(tool.HostRoot, toolRoot); err != nil {
				return fmt.Errorf("link local tool %s: %w", tool.Name, err)
			}
		default:
			if err := os.MkdirAll(toolRoot, 0o755); err != nil {
				return fmt.Errorf("prepare local tool mount %s: %w", tool.Name, err)
			}
		}

		for _, cmd := range tool.Commands {
			if err := writeLocalToolWrapper(filepath.Join(binDir, cmd.Name), tool.Name, cmd.RelativePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l CommandLauncher) prepareBwrapState(spec LaunchSpec) error {
	binDir := filepath.Join(spec.StateDir, "bin")
	configDir := filepath.Join(spec.StateDir, "config")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	if err := copyFile(l.ShelleyBinary, filepath.Join(binDir, "shelley"), 0o755); err != nil {
		return fmt.Errorf("copy shelley binary into bwrap state: %w", err)
	}
	if l.ConfigPath != "" {
		if err := copyFile(l.ConfigPath, filepath.Join(configDir, "shelley.json"), 0o644); err != nil {
			return fmt.Errorf("copy config into bwrap state: %w", err)
		}
	}
	return nil
}

func (l CommandLauncher) bwrapRuntimeFSArgs(spec LaunchSpec) []string {
	paths := []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc", "/opt"}
	args := make([]string, 0, len(paths)*2+8)
	args = append(args, "--dir", bwrapSandboxRoot)
	for _, p := range paths {
		fsArgs, ok := bwrapMountArgsForPath(p)
		if ok {
			args = append(args, fsArgs...)
		}
	}
	return args
}

func (l CommandLauncher) runtimeEnv(base []string, spec LaunchSpec, sandboxed bool) []string {
	env := append([]string{}, base...)
	env = append(env, "WORKSPACE_NAME="+spec.Name)
	toolsDir := filepath.Join(spec.StateDir, "tools")
	if sandboxed {
		toolsDir = runtimeSharedToolsDir
	}
	env = append(env, "WORKSPACE_TOOLS_DIR="+toolsDir)
	pathValue := prependPath(filepath.ToSlash(filepath.Join(toolsDir, "bin")), os.Getenv("PATH"))
	return append(env, "PATH="+pathValue)
}

func (l CommandLauncher) bwrapConfigPath(spec LaunchSpec) string {
	if l.ConfigPath == "" {
		return ""
	}
	return bwrapSandboxConfig
}

func bwrapMountArgsForPath(src string) ([]string, bool) {
	info, err := os.Lstat(src)
	if err != nil {
		return nil, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return nil, false
		}
		return []string{"--symlink", target, src}, true
	}
	return []string{"--ro-bind", src, src}, true
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func writeLocalToolWrapper(path, toolName, relativeCommand string) error {
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"exec \"$WORKSPACE_TOOLS_DIR/" + toolName + "/" + filepath.ToSlash(relativeCommand) + "\" \"$@\"\n"
	return os.WriteFile(path, []byte(script), 0o755)
}

func prependPath(dir, current string) string {
	dir = strings.TrimSpace(dir)
	current = strings.TrimSpace(current)
	if dir == "" {
		return current
	}
	if current == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + current
}
