import type { WorkspaceFileNode } from "@/api/types";

export const WORKSPACE_FILE_PREVIEW_LIMIT_BYTES = 128 * 1024;

export type WorkspaceFilePreviewKind = "text" | "binary" | "too_large";

export function workspacePathSegments(path: string): string[] {
  return path.split("/").filter(Boolean);
}

export function parentWorkspacePath(path: string): string {
  const parts = workspacePathSegments(path);
  if (parts.length === 0) return "";
  parts.pop();
  return parts.join("/");
}

export function joinWorkspacePath(base: string, name: string): string {
  const cleanName = name.replace(/^\/+/, "").trim();
  if (!cleanName) return base;
  return base ? `${base}/${cleanName}` : cleanName;
}

export function formatWorkspaceFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function isTextPreviewable(mimeType?: string): boolean {
  if (!mimeType) return true;
  const lower = mimeType.toLowerCase();
  if (lower.startsWith("text/")) return true;
  return (
    lower.includes("json") ||
    lower.includes("xml") ||
    lower.includes("javascript") ||
    lower.includes("typescript") ||
    lower.includes("svg") ||
    lower.includes("yaml") ||
    lower.includes("toml") ||
    lower.includes("x-sh")
  );
}

export function previewKindForFile(node: WorkspaceFileNode): WorkspaceFilePreviewKind {
  if (node.size > WORKSPACE_FILE_PREVIEW_LIMIT_BYTES) {
    return "too_large";
  }
  return isTextPreviewable(node.mimeType) ? "text" : "binary";
}
