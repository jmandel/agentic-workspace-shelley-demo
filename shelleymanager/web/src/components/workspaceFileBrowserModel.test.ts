import { describe, expect, test } from "bun:test";
import type { WorkspaceFileNode } from "@/api/types";
import {
  formatWorkspaceFileSize,
  joinWorkspacePath,
  parentWorkspacePath,
  previewKindForFile,
  workspacePathSegments,
} from "./workspaceFileBrowserModel";

function fileNode(overrides: Partial<WorkspaceFileNode>): WorkspaceFileNode {
  return {
    path: "docs/note.txt",
    name: "note.txt",
    kind: "file",
    size: 42,
    modifiedAt: "2026-03-11T00:00:00Z",
    mimeType: "text/plain",
    ...overrides,
  };
}

describe("workspace file browser model", () => {
  test("builds relative workspace paths", () => {
    expect(joinWorkspacePath("", "note.txt")).toBe("note.txt");
    expect(joinWorkspacePath("docs", "note.txt")).toBe("docs/note.txt");
    expect(joinWorkspacePath("docs", "/note.txt")).toBe("docs/note.txt");
  });

  test("derives parent workspace paths", () => {
    expect(parentWorkspacePath("")).toBe("");
    expect(parentWorkspacePath("docs")).toBe("");
    expect(parentWorkspacePath("docs/note.txt")).toBe("docs");
    expect(parentWorkspacePath("docs/nested/note.txt")).toBe("docs/nested");
  });

  test("splits workspace path segments", () => {
    expect(workspacePathSegments("")).toEqual([]);
    expect(workspacePathSegments("docs/nested/note.txt")).toEqual([
      "docs",
      "nested",
      "note.txt",
    ]);
  });

  test("formats file sizes", () => {
    expect(formatWorkspaceFileSize(512)).toBe("512 B");
    expect(formatWorkspaceFileSize(1536)).toBe("1.5 KB");
    expect(formatWorkspaceFileSize(2 * 1024 * 1024)).toBe("2.0 MB");
  });

  test("chooses preview mode from file metadata", () => {
    expect(previewKindForFile(fileNode({ mimeType: "text/plain", size: 128 }))).toBe(
      "text",
    );
    expect(previewKindForFile(fileNode({ mimeType: "image/png", size: 128 }))).toBe(
      "binary",
    );
    expect(
      previewKindForFile(
        fileNode({ mimeType: "text/plain", size: 200 * 1024 }),
      ),
    ).toBe("too_large");
  });
});
