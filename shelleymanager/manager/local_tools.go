package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const defaultLocalToolsCatalogName = "catalog.json"

type LocalTool struct {
	Name         string
	Version      string
	Description  string
	Guidance     string
	Requirements []string
	HostRoot     string
	Commands     []LocalToolCommand
}

type LocalToolCommand struct {
	Name         string
	RelativePath string
}

type localToolCatalogEntry struct {
	Name         string                    `json:"name"`
	Version      string                    `json:"version,omitempty"`
	Description  string                    `json:"description"`
	Guidance     string                    `json:"guidance,omitempty"`
	Requirements []string                  `json:"requirements,omitempty"`
	Root         string                    `json:"root,omitempty"`
	Commands     []localToolCatalogCommand `json:"commands"`
	Artifacts    []localToolCatalogArtifact `json:"artifacts,omitempty"`
}

type localToolCatalogCommand struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath,omitempty"`
}

type localToolCatalogArtifact struct {
	URL          string `json:"url"`
	RelativePath string `json:"relativePath"`
	SHA256       string `json:"sha256,omitempty"`
	Executable   bool   `json:"executable,omitempty"`
}

type localToolInfo struct {
	Name         string                 `json:"name"`
	Kind         string                 `json:"kind"`
	Exposure     string                 `json:"exposure"`
	Description  string                 `json:"description"`
	Commands     []localToolCommandInfo `json:"commands"`
	Guidance     string                 `json:"guidance,omitempty"`
	Requirements []string               `json:"requirements,omitempty"`
	Version      string                 `json:"version,omitempty"`
}

type localToolCommandInfo struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

func LoadLocalToolsCatalog(sharedToolsDir, explicitPath, cacheRoot string) ([]LocalTool, error) {
	sharedToolsDir = strings.TrimSpace(sharedToolsDir)
	if sharedToolsDir == "" {
		return nil, nil
	}

	catalogPath := strings.TrimSpace(explicitPath)
	if catalogPath == "" {
		candidate := filepath.Join(sharedToolsDir, defaultLocalToolsCatalogName)
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("stat local tools catalog: %w", err)
		}
		catalogPath = candidate
	}

	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, fmt.Errorf("read local tools catalog: %w", err)
	}

	var entries []localToolCatalogEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode local tools catalog: %w", err)
	}

	tools := make([]LocalTool, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Description = strings.TrimSpace(entry.Description)
		entry.Guidance = strings.TrimSpace(entry.Guidance)
		entry.Root = strings.TrimSpace(entry.Root)
		if entry.Name == "" {
			return nil, fmt.Errorf("local tool name required")
		}
		if _, ok := seen[entry.Name]; ok {
			return nil, fmt.Errorf("duplicate local tool name %q", entry.Name)
		}
		seen[entry.Name] = struct{}{}

		root := entry.Root
		if root == "" {
			root = entry.Name
		}
		sourceRoot := filepath.Join(sharedToolsDir, filepath.Clean(root))
		info, err := os.Stat(sourceRoot)
		if err != nil {
			return nil, fmt.Errorf("local tool %q root %q: %w", entry.Name, sourceRoot, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("local tool %q root %q is not a directory", entry.Name, sourceRoot)
		}

		hostRoot := sourceRoot
		if len(entry.Artifacts) > 0 {
			resolvedRoot, err := materializeLocalToolBundle(sourceRoot, cacheRoot, entry)
			if err != nil {
				return nil, fmt.Errorf("materialize local tool %q: %w", entry.Name, err)
			}
			hostRoot = resolvedRoot
		}

		commands := make([]LocalToolCommand, 0, len(entry.Commands))
		for _, cmd := range entry.Commands {
			cmd.Name = strings.TrimSpace(cmd.Name)
			cmd.RelativePath = strings.TrimSpace(cmd.RelativePath)
			if cmd.Name == "" {
				return nil, fmt.Errorf("local tool %q command name required", entry.Name)
			}
			if cmd.RelativePath == "" {
				cmd.RelativePath = filepath.ToSlash(filepath.Join("bin", cmd.Name))
			}
			commands = append(commands, LocalToolCommand{
				Name:         cmd.Name,
				RelativePath: filepath.Clean(cmd.RelativePath),
			})
			commandPath := filepath.Join(hostRoot, filepath.Clean(cmd.RelativePath))
			commandInfo, err := os.Stat(commandPath)
			if err != nil {
				return nil, fmt.Errorf("local tool %q command %q: %w", entry.Name, commandPath, err)
			}
			if commandInfo.IsDir() {
				return nil, fmt.Errorf("local tool %q command %q is a directory", entry.Name, commandPath)
			}
		}
		if len(commands) == 0 {
			return nil, fmt.Errorf("local tool %q requires at least one command", entry.Name)
		}

		requirements := append([]string(nil), entry.Requirements...)
		slices.Sort(requirements)

		tools = append(tools, LocalTool{
			Name:         entry.Name,
			Version:      strings.TrimSpace(entry.Version),
			Description:  entry.Description,
			Guidance:     entry.Guidance,
			Requirements: requirements,
			HostRoot:     hostRoot,
			Commands:     commands,
		})
	}

	slices.SortFunc(tools, func(a, b LocalTool) int {
		return strings.Compare(a.Name, b.Name)
	})
	return tools, nil
}

func ResolveLocalTools(catalog []LocalTool, names []string) ([]LocalTool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	byName := make(map[string]LocalTool, len(catalog))
	for _, tool := range catalog {
		byName[tool.Name] = tool
	}

	selected := make([]LocalTool, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, fmt.Errorf("local tool name required")
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		tool, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown local tool %q", name)
		}
		selected = append(selected, tool)
	}

	slices.SortFunc(selected, func(a, b LocalTool) int {
		return strings.Compare(a.Name, b.Name)
	})
	return selected, nil
}

func localToolInfos(tools []LocalTool) []localToolInfo {
	if len(tools) == 0 {
		return nil
	}
	infos := make([]localToolInfo, 0, len(tools))
	for _, tool := range tools {
		commands := make([]localToolCommandInfo, 0, len(tool.Commands))
		for _, cmd := range tool.Commands {
			commands = append(commands, localToolCommandInfo{
				Name:    cmd.Name,
				Command: "/tools/bin/" + cmd.Name,
			})
		}
		infos = append(infos, localToolInfo{
			Name:         tool.Name,
			Kind:         "local_tool",
			Exposure:     "bash_only",
			Description:  tool.Description,
			Commands:     commands,
			Guidance:     tool.Guidance,
			Requirements: append([]string(nil), tool.Requirements...),
			Version:      toolVersion(tool),
		})
	}
	return infos
}

func toolVersion(tool LocalTool) string {
	if strings.TrimSpace(tool.Version) == "" {
		return "manager"
	}
	return strings.TrimSpace(tool.Version)
}

func materializeLocalToolBundle(sourceRoot, cacheRoot string, entry localToolCatalogEntry) (string, error) {
	cacheRoot = strings.TrimSpace(cacheRoot)
	if cacheRoot == "" {
		return "", fmt.Errorf("cache root required for local tool artifacts")
	}

	resolvedRoot := filepath.Join(cacheRoot, cacheDirName(entry.Name, entry.Version))
	if err := os.MkdirAll(resolvedRoot, 0o755); err != nil {
		return "", err
	}
	if err := copyDirContents(sourceRoot, resolvedRoot); err != nil {
		return "", err
	}
	for _, artifact := range entry.Artifacts {
		if err := ensureLocalToolArtifact(resolvedRoot, artifact); err != nil {
			return "", err
		}
	}
	return resolvedRoot, nil
}

func cacheDirName(name, version string) string {
	name = sanitizeCacheComponent(name)
	version = sanitizeCacheComponent(version)
	if version == "" {
		return name
	}
	return name + "-" + version
}

func sanitizeCacheComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "tool"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func copyDirContents(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func ensureLocalToolArtifact(root string, artifact localToolCatalogArtifact) error {
	rel := filepath.Clean(strings.TrimSpace(artifact.RelativePath))
	if rel == "." || rel == "" || rel == string(filepath.Separator) {
		return fmt.Errorf("artifact relativePath required")
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("artifact relativePath %q escapes bundle root", artifact.RelativePath)
	}
	target := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if ok, err := localToolArtifactValid(target, artifact); err == nil && ok {
		return nil
	} else if err != nil {
		return err
	}
	return downloadLocalToolArtifact(target, artifact)
}

func localToolArtifactValid(target string, artifact localToolCatalogArtifact) (bool, error) {
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() || info.Size() == 0 {
		return false, nil
	}
	if checksum := strings.TrimSpace(artifact.SHA256); checksum != "" {
		sum, err := sha256File(target)
		if err != nil {
			return false, err
		}
		if !strings.EqualFold(sum, checksum) {
			return false, nil
		}
	}
	if artifact.Executable {
		if err := os.Chmod(target, 0o755); err != nil {
			return false, err
		}
	}
	return true, nil
}

func downloadLocalToolArtifact(target string, artifact localToolCatalogArtifact) error {
	url := strings.TrimSpace(artifact.URL)
	if url == "" {
		return fmt.Errorf("artifact url required for %q", artifact.RelativePath)
	}
	tmp := target + ".tmp"
	_ = os.Remove(tmp)

	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %q: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		io.Copy(io.Discard, res.Body)
		return fmt.Errorf("download %q returned %d", url, res.StatusCode)
	}

	mode := os.FileMode(0o644)
	if artifact.Executable {
		mode = 0o755
	}
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, res.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if checksum := strings.TrimSpace(artifact.SHA256); checksum != "" {
		sum, err := sha256File(tmp)
		if err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if !strings.EqualFold(sum, checksum) {
			_ = os.Remove(tmp)
			return fmt.Errorf("downloaded artifact %q checksum mismatch: got %s want %s", artifact.RelativePath, sum, checksum)
		}
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if artifact.Executable {
		return os.Chmod(target, 0o755)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeWorkspaceLocalToolGuidance(workspaceDir string, tools []LocalTool) error {
	if len(tools) == 0 {
		return nil
	}

	guidanceDir := filepath.Join(workspaceDir, ".shelley")
	if err := os.MkdirAll(guidanceDir, 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Workspace Local Tools\n\n")
	b.WriteString("This workspace includes trusted local tools provided by the Shelley Manager.\n")
	b.WriteString("These tools are available through bash and are already approved for this workspace.\n\n")
	b.WriteString("Available local tools:\n")
	for _, tool := range tools {
		b.WriteString("- `")
		b.WriteString(tool.Name)
		b.WriteString("`")
		if tool.Description != "" {
			b.WriteString(": ")
			b.WriteString(tool.Description)
		}
		b.WriteByte('\n')
		for _, cmd := range tool.Commands {
			b.WriteString("  command: `")
			b.WriteString(cmd.Name)
			b.WriteString("`\n")
		}
		if tool.Guidance != "" {
			b.WriteString("  note: ")
			b.WriteString(tool.Guidance)
			b.WriteByte('\n')
		}
		if len(tool.Requirements) > 0 {
			b.WriteString("  requires: ")
			b.WriteString(strings.Join(tool.Requirements, ", "))
			b.WriteByte('\n')
		}
	}
	b.WriteString("\nUse these through bash when appropriate. Prefer the exact command names above.\n")

	return os.WriteFile(filepath.Join(guidanceDir, "AGENTS.md"), []byte(b.String()), 0o644)
}
