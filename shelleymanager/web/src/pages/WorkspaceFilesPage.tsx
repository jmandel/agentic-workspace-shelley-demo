import { Link, useParams } from "wouter";
import { useStore } from "@/store";
import { AboutLink } from "@/components/AboutLink";
import { ParticipantNameInput } from "@/components/ParticipantNameInput";
import { WorkspaceFileBrowser } from "@/components/WorkspaceFileBrowser";
import { workspacePageHref } from "@/navigation";

export function WorkspaceFilesPage() {
  const params = useParams<{
    namespace: string;
    workspace: string;
  }>();
  const namespace = params.namespace ?? "";
  const workspace = params.workspace ?? "";

  const workspaceDetail = useStore((s) =>
    s.workspaces.find(
      (ws) => ws.name === workspace && (ws.namespace ?? namespace) === namespace,
    ),
  );

  return (
    <div className="page">
      <div className="card">
        <div className="row row-between" style={{ gap: 12 }}>
          <div className="topic-breadcrumbs">
            <Link
              href="/"
              style={{
                color: "var(--muted)",
                textDecoration: "none",
                fontSize: 13,
              }}
            >
              Workspaces
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>
              /
            </span>
            <Link
              href={workspacePageHref(namespace, workspace)}
              style={{
                fontSize: 13,
                fontWeight: 500,
                textDecoration: "none",
                color: "inherit",
              }}
            >
              {workspace}
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>
              /
            </span>
            <span style={{ fontSize: 13, fontWeight: 600 }}>files</span>
          </div>
          <div className="row" style={{ gap: 6 }}>
            <AboutLink />
            <ParticipantNameInput compact />
            {workspaceDetail && (
              <>
                <span className="status-dot" data-status={workspaceDetail.status} />
                <span className="muted" style={{ fontSize: 12 }}>
                  {workspaceDetail.status}
                </span>
              </>
            )}
          </div>
        </div>
        <div className="stack-xs" style={{ marginTop: 10 }}>
          <h1 style={{ margin: 0 }}>Workspace Files</h1>
          <p className="muted" style={{ margin: 0 }}>
            These files belong to the workspace, not to any single topic.
          </p>
          <p className="muted" style={{ margin: 0 }}>
            Every topic in <code>{workspace}</code> sees the same workspace files,
            so browsing or uploading here affects the shared working set for the
            whole workspace.
          </p>
          <div className="row" style={{ gap: 6, marginTop: 4 }}>
            <Link
              href={workspacePageHref(namespace, workspace)}
              className="btn btn-secondary btn-sm"
            >
              Workspace Overview
            </Link>
          </div>
        </div>
      </div>

      <div style={{ marginTop: 16 }}>
        <WorkspaceFileBrowser
          namespace={namespace}
          workspace={workspace}
          browserId={`workspace-page:${namespace}:${workspace}`}
          title="Workspace Files"
        />
      </div>
    </div>
  );
}
