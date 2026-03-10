package manager

import (
	"bytes"
	"compress/gzip"
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

func TestLoadLocalToolsCatalogSupportsSupportBundleAndGzipArtifacts(t *testing.T) {
	sharedDir := t.TempDir()
	sourceRoot := filepath.Join(sharedDir, "hl7-jira-support")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(sourceRoot, "bin", "hl7-jira-mcp.js")
	if err := os.WriteFile(scriptPath, []byte("#!/usr/bin/env bun\nconsole.log('ok')\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dbBytes := []byte("sqlite bytes for jira support bundle test")
	dbSum := sha256.Sum256(dbBytes)
	dbChecksum := hex.EncodeToString(dbSum[:])

	var gz bytes.Buffer
	gzw := gzip.NewWriter(&gz)
	if _, err := gzw.Write(dbBytes); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	gzSum := sha256.Sum256(gz.Bytes())
	gzChecksum := hex.EncodeToString(gzSum[:])

	artifactServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(gz.Bytes())
	}))
	defer artifactServer.Close()

	catalog := []map[string]any{
		{
			"name":        "hl7-jira-support",
			"version":     "2026-01-22",
			"exposure":    "support_bundle",
			"description": "Support bundle for the Jira MCP server",
			"root":        "hl7-jira-support",
			"artifacts": []map[string]any{
				{
					"url":          artifactServer.URL + "/jira-data.db.gz",
					"relativePath": "data/jira-data.db",
					"compression":  "gzip",
					"sourceSha256": gzChecksum,
					"sha256":       dbChecksum,
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
	if tools[0].Exposure != localToolExposureSupportBundle {
		t.Fatalf("unexpected exposure %q", tools[0].Exposure)
	}
	if len(tools[0].Commands) != 0 {
		t.Fatalf("expected no commands for support bundle, got %#v", tools[0].Commands)
	}

	dbPath := filepath.Join(tools[0].HostRoot, "data", "jira-data.db")
	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(dbBytes) {
		t.Fatalf("unexpected decompressed bundle artifact %q", string(got))
	}

	info := localToolInfos(tools)
	if len(info) != 1 || info[0].Exposure != localToolExposureSupportBundle {
		t.Fatalf("unexpected local tool info %#v", info)
	}
	if len(info[0].Commands) != 0 {
		t.Fatalf("expected no commands in info for support bundle, got %#v", info[0].Commands)
	}
}
