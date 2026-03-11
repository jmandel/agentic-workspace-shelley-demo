export function workspacePageHref(namespace: string, workspace: string): string {
  return `/app/${encodeURIComponent(namespace)}/${encodeURIComponent(workspace)}`;
}

export function topicPageHref(
  namespace: string,
  workspace: string,
  topic: string,
): string {
  return `${workspacePageHref(namespace, workspace)}/${encodeURIComponent(topic)}`;
}

export function workspaceFilesPageHref(
  namespace: string,
  workspace: string,
): string {
  return `${workspacePageHref(namespace, workspace)}/files`;
}
