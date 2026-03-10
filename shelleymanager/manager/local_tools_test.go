package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLocalToolsCatalogMaterializesArtifacts(t *testing.T) {
	sharedDir := t.TempDir()
	sourceRoot := filepath.Join(sharedDir, "fhir-validator")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "bin", "fhir-validator"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	jarBytes := []byte("validator jar bytes for test")
	sum := sha256.Sum256(jarBytes)
	checksum := hex.EncodeToString(sum[:])
	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(jarBytes)
	}))
	defer artifactServer.Close()

	catalog := []map[string]any{
		{
			"name":        "fhir-validator",
			"version":     "6.8.2",
			"description": "FHIR Validator CLI",
			"root":        "fhir-validator",
			"commands": []map[string]any{
				{"name": "fhir-validator"},
			},
			"artifacts": []map[string]any{
				{
					"url":          artifactServer.URL + "/validator_cli.jar",
					"relativePath": "validator_cli.jar",
					"sha256":       checksum,
				},
			},
		},
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "catalog.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cacheRoot := filepath.Join(t.TempDir(), "cache")
	tools, err := LoadLocalToolsCatalog(sharedDir, "", cacheRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0]
	if tool.Version != "6.8.2" {
		t.Fatalf("unexpected tool version %q", tool.Version)
	}
	if !strings.HasPrefix(tool.HostRoot, cacheRoot) {
		t.Fatalf("expected materialized tool root under %q, got %q", cacheRoot, tool.HostRoot)
	}

	jarPath := filepath.Join(tool.HostRoot, "validator_cli.jar")
	jarOnDisk, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(jarOnDisk) != string(jarBytes) {
		t.Fatalf("unexpected artifact bytes %q", string(jarOnDisk))
	}

	wrapperPath := filepath.Join(tool.HostRoot, "bin", "fhir-validator")
	if _, err := os.Stat(wrapperPath); err != nil {
		t.Fatalf("expected copied wrapper script: %v", err)
	}

	info := localToolInfos(tools)
	if len(info) != 1 || info[0].Version != "6.8.2" {
		t.Fatalf("unexpected local tool info %#v", info)
	}
}
