package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const workspaceMetadataFilename = "workspace.json"

type workspaceMetadata struct {
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	CreatedAt  string   `json:"createdAt,omitempty"`
	Template   string   `json:"template,omitempty"`
	LocalTools []string `json:"localTools,omitempty"`
}

func (m *Manager) RecoverWorkspaces(ctx context.Context) (int, error) {
	if strings.TrimSpace(m.stateRoot) == "" {
		return 0, nil
	}

	namespaceEntries, err := os.ReadDir(m.stateRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	recovered := 0
	var errs []error
	for _, namespaceEntry := range namespaceEntries {
		if !namespaceEntry.IsDir() {
			continue
		}
		namespace := strings.TrimSpace(namespaceEntry.Name())
		if namespace == "" || strings.HasPrefix(namespace, ".") {
			continue
		}
		if err := validateName(namespace); err != nil {
			errs = append(errs, fmt.Errorf("skip namespace %q: %w", namespace, err))
			continue
		}

		namespaceDir := filepath.Join(m.stateRoot, namespace)
		workspaceEntries, err := os.ReadDir(namespaceDir)
		if err != nil {
			errs = append(errs, fmt.Errorf("read namespace %q: %w", namespace, err))
			continue
		}

		for _, workspaceEntry := range workspaceEntries {
			if !workspaceEntry.IsDir() {
				continue
			}
			name := strings.TrimSpace(workspaceEntry.Name())
			if name == "" || strings.HasPrefix(name, ".") {
				continue
			}
			if err := validateName(name); err != nil {
				errs = append(errs, fmt.Errorf("skip workspace %q/%q: %w", namespace, name, err))
				continue
			}
			recoverable, err := isRecoverableWorkspaceDir(filepath.Join(namespaceDir, name))
			if err != nil {
				errs = append(errs, fmt.Errorf("inspect workspace %q/%q: %w", namespace, name, err))
				continue
			}
			if !recoverable {
				continue
			}

			if err := m.recoverWorkspace(ctx, namespace, name); err != nil {
				errs = append(errs, fmt.Errorf("%s/%s: %w", namespace, name, err))
				continue
			}
			recovered++
		}
	}

	return recovered, errors.Join(errs...)
}

func (m *Manager) recoverWorkspace(ctx context.Context, namespace, name string) error {
	if _, exists := m.getWorkspace(namespace, name); exists {
		return nil
	}

	spec, err := m.launcher.WorkspacePaths(namespace, name)
	if err != nil {
		return err
	}

	metadata, err := m.loadWorkspaceMetadata(spec.StateDir, namespace, name)
	if err != nil {
		return err
	}

	localTools, err := ResolveLocalTools(m.localTools, metadata.LocalTools)
	if err != nil {
		return err
	}
	spec.LocalTools = append([]LocalTool(nil), localTools...)

	if err := writeWorkspaceLocalToolGuidance(spec.WorkspaceDir, localTools); err != nil {
		return fmt.Errorf("write local tool guidance: %w", err)
	}

	runtime, err := m.launcher.Launch(ctx, spec)
	if err != nil {
		return err
	}

	createdAt, err := parseWorkspaceCreatedAt(spec.StateDir, metadata.CreatedAt)
	if err != nil {
		if runtime.Stop != nil {
			_ = runtime.Stop(context.Background())
		}
		return err
	}

	ws := &Workspace{
		Namespace:  namespace,
		Name:       name,
		StateDir:   spec.StateDir,
		CreatedAt:  createdAt,
		Template:   strings.TrimSpace(metadata.Template),
		LocalTools: append([]LocalTool(nil), localTools...),
		Runtime:    *runtime,
	}

	m.mu.Lock()
	m.workspaces[workspaceKey{namespace: namespace, name: name}] = ws
	m.mu.Unlock()

	if err := m.persistWorkspaceMetadata(ws); err != nil {
		m.logger.Warn("failed to persist recovered workspace metadata", "namespace", namespace, "name", name, "error", err)
	}
	return nil
}

func (m *Manager) persistWorkspaceMetadata(ws *Workspace) error {
	if ws == nil || strings.TrimSpace(ws.StateDir) == "" {
		return nil
	}

	localToolNames := make([]string, 0, len(ws.LocalTools))
	for _, tool := range ws.LocalTools {
		localToolNames = append(localToolNames, tool.Name)
	}
	slices.Sort(localToolNames)

	meta := workspaceMetadata{
		Namespace:  ws.Namespace,
		Name:       ws.Name,
		CreatedAt:  ws.CreatedAt.UTC().Format(time.RFC3339),
		Template:   strings.TrimSpace(ws.Template),
		LocalTools: localToolNames,
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ws.StateDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ws.StateDir, workspaceMetadataFilename), raw, 0o644)
}

func (m *Manager) loadWorkspaceMetadata(stateDir, namespace, name string) (workspaceMetadata, error) {
	metaPath := filepath.Join(stateDir, workspaceMetadataFilename)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return workspaceMetadata{}, err
		}
		return inferredWorkspaceMetadata(stateDir, namespace, name)
	}

	var meta workspaceMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return workspaceMetadata{}, fmt.Errorf("decode %s: %w", metaPath, err)
	}
	if strings.TrimSpace(meta.Namespace) == "" {
		meta.Namespace = namespace
	}
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = name
	}
	if meta.Namespace != namespace || meta.Name != name {
		return workspaceMetadata{}, fmt.Errorf("metadata mismatch: expected %s/%s, got %s/%s", namespace, name, meta.Namespace, meta.Name)
	}
	return meta, nil
}

func inferredWorkspaceMetadata(stateDir, namespace, name string) (workspaceMetadata, error) {
	info, err := os.Stat(stateDir)
	if err != nil {
		return workspaceMetadata{}, err
	}
	return workspaceMetadata{
		Namespace: namespace,
		Name:      name,
		CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
	}, nil
}

func parseWorkspaceCreatedAt(stateDir, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		info, err := os.Stat(stateDir)
		if err != nil {
			return time.Time{}, err
		}
		return info.ModTime().UTC(), nil
	}
	createdAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse createdAt %q: %w", value, err)
	}
	return createdAt.UTC(), nil
}

func stateDirHasEntries(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return len(entries) > 0, nil
}

func isRecoverableWorkspaceDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case workspaceMetadataFilename, "workspace", "shelley.db", "tools", "runtime.log":
			return true, nil
		}
	}
	return false, nil
}
