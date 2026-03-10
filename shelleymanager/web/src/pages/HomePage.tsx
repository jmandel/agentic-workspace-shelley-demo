import { useEffect } from "react";
import { Link } from "wouter";
import { useStore } from "@/store";
import { ParticipantNameInput } from "@/components/ParticipantNameInput";
import { CreateWorkspaceForm } from "@/components/CreateWorkspaceForm";
import { WorkspaceCard } from "@/components/WorkspaceCard";

export function HomePage() {
  const workspaces = useStore((s) => s.workspaces);
  const workspacesLoading = useStore((s) => s.workspacesLoading);
  const fetchWorkspaces = useStore((s) => s.fetchWorkspaces);
  const fetchLocalTools = useStore((s) => s.fetchLocalTools);
  const fetchHealth = useStore((s) => s.fetchHealth);
  const eventsStatus = useStore((s) => s.eventsStatus);

  useEffect(() => {
    // Discover namespace before fetching namespace-scoped data
    fetchHealth().then(() => {
      fetchLocalTools();
      fetchWorkspaces();
      // Also connect manager events WS for live updates.
      const { connectManagerEvents, namespace } = useStore.getState();
      connectManagerEvents(namespace);
    });
    return () => {
      useStore.getState().disconnectManagerEvents();
    };
  }, [fetchHealth, fetchLocalTools, fetchWorkspaces]);

  return (
    <div className="page">
      {/* Header */}
      <div className="card">
        <div className="row row-between">
          <h1 style={{ margin: 0 }}>Workspace Manager</h1>
          <div className="row" style={{ gap: 6 }}>
            <ParticipantNameInput compact />
            <Link href="/ws-language" className="btn btn-secondary btn-sm">
              WS Language
            </Link>
          </div>
        </div>
      </div>

      {/* Two columns — both are cards */}
      <div className="grid-2">
        <section className="card">
          <h2>Create Workspace</h2>
          <CreateWorkspaceForm onCreated={fetchWorkspaces} />
        </section>

        <section className="card">
          <div className="row row-between" style={{ marginBottom: 8 }}>
            <h2 style={{ margin: 0 }}>
              Workspaces
              <span
                className="status-dot"
                data-status={eventsStatus === "connected" ? "running" : "unavailable"}
                title={`Live updates: ${eventsStatus}`}
                style={{ marginLeft: 6 }}
              />
            </h2>
            <button
              className="btn btn-secondary btn-sm"
              onClick={fetchWorkspaces}
              disabled={workspacesLoading}
            >
              {workspacesLoading ? "Loading..." : "Refresh"}
            </button>
          </div>
          {workspaces.length === 0 ? (
            <p className="muted" style={{ fontSize: 13, margin: 0 }}>
              {workspacesLoading ? "Loading..." : "No workspaces yet."}
            </p>
          ) : (
            <div className="ws-list">
              {workspaces.map((ws) => (
                <WorkspaceCard key={ws.name} workspace={ws} />
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
