package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const defaultLocalToolsCatalogName = "catalog.json"

type LocalTool struct {
	Name         string
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
	Description  string                    `json:"description"`
	Guidance     string                    `json:"guidance,omitempty"`
	Requirements []string                  `json:"requirements,omitempty"`
	Root         string                    `json:"root,omitempty"`
	Commands     []localToolCatalogCommand `json:"commands"`
}

type localToolCatalogCommand struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath,omitempty"`
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

func LoadLocalToolsCatalog(sharedToolsDir, explicitPath string) ([]LocalTool, error) {
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
		hostRoot := filepath.Join(sharedToolsDir, filepath.Clean(root))
		info, err := os.Stat(hostRoot)
		if err != nil {
			return nil, fmt.Errorf("local tool %q root %q: %w", entry.Name, hostRoot, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("local tool %q root %q is not a directory", entry.Name, hostRoot)
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
		}
		if len(commands) == 0 {
			return nil, fmt.Errorf("local tool %q requires at least one command", entry.Name)
		}

		requirements := append([]string(nil), entry.Requirements...)
		slices.Sort(requirements)

		tools = append(tools, LocalTool{
			Name:         entry.Name,
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
			Version:      "demo",
		})
	}
	return infos
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
