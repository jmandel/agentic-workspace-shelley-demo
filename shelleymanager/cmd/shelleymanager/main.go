package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"workspace-protocol/shelleymanager/manager"
)

func main() {
	var cfg struct {
		Listen          string
		PortFile        string
		Namespace       string
		StateDir        string
		RuntimeMode     string
		ShelleyBinary   string
		DockerBinary    string
		DockerImage     string
		DockerCommand   string
		BwrapBinary     string
		DefaultModel    string
		PredictableOnly bool
		ConfigPath      string
		Debug           bool
	}

	flag.StringVar(&cfg.Listen, "listen", "127.0.0.1:31337", "Address to listen on")
	flag.StringVar(&cfg.PortFile, "port-file", "", "Write actual listening port to this file")
	flag.StringVar(&cfg.Namespace, "namespace", "default", "Default namespace for compatibility /workspaces routes")
	flag.StringVar(&cfg.StateDir, "state-dir", ".shelleymanager-state", "State root for manager metadata, logs, and runtime workspace dirs")
	flag.StringVar(&cfg.RuntimeMode, "runtime-mode", "process", "Runtime launch mode: process, docker, or bwrap")
	flag.StringVar(&cfg.ShelleyBinary, "shelley-binary", "", "Path to Shelley binary for process/bwrap launch modes")
	flag.StringVar(&cfg.DockerBinary, "docker-binary", "docker", "Docker CLI binary")
	flag.StringVar(&cfg.DockerImage, "docker-image", "", "Shelley runtime image for docker mode")
	flag.StringVar(&cfg.DockerCommand, "docker-command", "shelley", "Command inside the docker image that starts Shelley")
	flag.StringVar(&cfg.BwrapBinary, "bwrap-binary", "bwrap", "bubblewrap binary for bwrap mode")
	flag.StringVar(&cfg.DefaultModel, "default-model", "predictable", "Default model forwarded to Shelley runtimes")
	flag.BoolVar(&cfg.PredictableOnly, "predictable-only", false, "Launch Shelley runtimes in predictable-only mode")
	flag.StringVar(&cfg.ConfigPath, "config", "", "Optional shelley.json path to pass through to runtimes")
	flag.BoolVar(&cfg.Debug, "debug", false, "Enable debug logging")
	flag.Parse()

	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	launcher := manager.CommandLauncher{
		Mode:            cfg.RuntimeMode,
		StateRoot:       cfg.StateDir,
		ShelleyBinary:   cfg.ShelleyBinary,
		DockerBinary:    cfg.DockerBinary,
		DockerImage:     cfg.DockerImage,
		DockerCommand:   cfg.DockerCommand,
		BwrapBinary:     cfg.BwrapBinary,
		DefaultModel:    cfg.DefaultModel,
		PredictableOnly: cfg.PredictableOnly,
		ConfigPath:      cfg.ConfigPath,
		DebugRuntime:    cfg.Debug,
	}

	mgr, err := manager.New(manager.Config{
		DefaultNamespace: cfg.Namespace,
		Launcher:         launcher,
		Logger:           logger,
	})
	if err != nil {
		logger.Error("failed to build manager", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		logger.Error("failed to create state dir", "path", cfg.StateDir, "error", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		logger.Error("failed to listen", "listen", cfg.Listen, "error", err)
		os.Exit(1)
	}
	if cfg.PortFile != "" {
		if addr, ok := listener.Addr().(*net.TCPAddr); ok {
			if err := os.WriteFile(cfg.PortFile, []byte(fmt.Sprintf("%d\n", addr.Port)), 0o644); err != nil {
				logger.Error("failed to write port file", "path", cfg.PortFile, "error", err)
				os.Exit(1)
			}
		}
	}

	server := &http.Server{
		Handler:           mgr,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = mgr.Shutdown(shutdownCtx)
	}()

	logger.Info("shelleymanager listening", "listen", listener.Addr().String(), "runtime_mode", cfg.RuntimeMode)
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		logger.Error("server exited", "error", err)
		os.Exit(1)
	}
}
