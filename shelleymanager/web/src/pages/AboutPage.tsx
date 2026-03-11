import { Link } from "wouter";
import { PageMenu } from "@/components/PageMenu";

export function AboutPage() {
  return (
    <div className="page page-narrow">
      <div className="card">
        <div className="row row-between" style={{ alignItems: "flex-start" }}>
          <div style={{ maxWidth: 720 }}>
            <h1>About This Demo</h1>
            <p className="muted">
              This site demonstrates a workspace protocol for collaborative
              agent work. A workspace is a shared project environment. A topic
              is a persistent thread of work inside that environment. People can
              join from the browser or CLI, watch the same live activity, and
              take turns steering the work forward.
            </p>
            <p className="muted" style={{ marginBottom: 0 }}>
              The point is not this specific manager or runtime. The point is a
              clean, interoperable model that different implementations could
              support consistently.
            </p>
          </div>
          <div className="row" style={{ gap: 6 }}>
            <PageMenu showAbout={false} />
            <Link href="/" className="btn btn-secondary">
              Back
            </Link>
          </div>
        </div>
      </div>

      <div className="grid-2" style={{ marginTop: 16 }}>
        <section className="card">
          <h2>Goals</h2>
          <div className="stack-sm">
            <p>
              Show a simple external shape for shared agent work: create a
              workspace, open a topic, connect multiple clients, and observe one
              coherent stream of activity.
            </p>
            <p>
              Keep the public contract focused on semantics rather than internal
              hosting details, so the same model can be implemented by more than
              one stack.
            </p>
          </div>
        </section>

        <section className="card">
          <h2>Protocol Shape</h2>
          <div className="stack-sm">
            <p>
              Workspaces hold shared state and enabled tools. Topics are the
              individual streams of discussion and execution inside a workspace.
            </p>
            <p>
              Clients interact through ordinary HTTP APIs plus live topic event
              streams. The system supports multi-client observation, queued
              prompts, approvals, tool activity, and interruption.
            </p>
          </div>
        </section>
      </div>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>Things To Try</h2>
        <div className="stack-sm">
          <p>Open the same topic in two browser tabs and watch events stay in sync.</p>
          <p>Open the CLI from a topic page and submit work from both clients.</p>
          <p>Queue a second prompt while the first is running, then reorder or delete it.</p>
          <p>Enable a tool and watch how tool calls appear alongside ordinary turns.</p>
          <p>Interrupt an active turn and confirm every client sees the same result.</p>
        </div>
      </section>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>Demo Story</h2>
        <div className="stack-sm">
          <p>
            This demo uses a small FHIR implementation-guide workspace. The
            agent can inspect the example files, run validation tools, consult
            Jira context, and explain what it found.
          </p>
          <p>
            The interesting part is not the FHIR content itself. It is that the
            work is visible and controllable as a shared session with stable
            workspace and topic concepts, rather than as one person’s local chat
            window.
          </p>
        </div>
      </section>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>What Is Still Missing</h2>
        <div className="stack-sm">
          <p>
            The protocol is still evolving. Some obvious next areas are richer
            streaming, multipart message content, resumable event cursors, and a
            cleaner artifact model.
          </p>
          <p className="muted">
            This demo is meant to make the shape of the system concrete enough
            to discuss and improve.
          </p>
        </div>
      </section>
    </div>
  );
}
