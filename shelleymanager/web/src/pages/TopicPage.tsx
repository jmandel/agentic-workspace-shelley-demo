import { useEffect, useState, useRef } from "react";
import { Link, useParams, useLocation } from "wouter";
import { useStore } from "@/store";
import { MessageList } from "@/components/MessageList";
import { QueuePanel } from "@/components/QueuePanel";
import { ParticipantNameInput } from "@/components/ParticipantNameInput";
import * as api from "@/api/client";

export function TopicPage() {
  const params = useParams<{
    namespace: string;
    workspace: string;
    topic: string;
  }>();
  const namespace = params.namespace ?? "";
  const workspace = params.workspace ?? "";
  const topic = params.topic ?? "";
  const [, navigate] = useLocation();

  const connectionStatus = useStore((s) => s.connectionStatus);
  const turnActive = useStore((s) => s.turnActive);
  const messages = useStore((s) => s.messages);
  const queue = useStore((s) => s.queue);
  const sendPrompt = useStore((s) => s.sendPrompt);
  const sendInterrupt = useStore((s) => s.sendInterrupt);

  const [prompt, setPrompt] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const { connectTopic, disconnectTopic } = useStore.getState();
    connectTopic(namespace, workspace, topic);
    return () => disconnectTopic();
  }, [namespace, workspace, topic]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = prompt.trim();
    if (!text || connectionStatus !== "connected") return;
    sendPrompt(text);
    setPrompt("");
    textareaRef.current?.focus();
  };

  const handleDeleteTopic = async () => {
    if (!confirm(`Delete topic "${topic}"?`)) return;
    await api.deleteTopic(namespace, workspace, topic);
    navigate("/");
  };

  const shelleyHref = `/shelley/${encodeURIComponent(namespace)}/${encodeURIComponent(workspace)}/${encodeURIComponent(topic)}`;
  const cliCommand = `WS_MANAGER=${window.location.origin} bun run cli.ts connect ${workspace} ${topic}`;

  return (
    <div className="page-topic">
      {/* Header */}
      <div className="card" style={{ flexShrink: 0 }}>
        <div className="row row-between" style={{ gap: 12 }}>
          <div className="topic-breadcrumbs">
            <Link href="/" style={{ color: "var(--muted)", textDecoration: "none", fontSize: 13 }}>
              Workspaces
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>/</span>
            <Link
              href={`/app/${encodeURIComponent(namespace)}/${encodeURIComponent(workspace)}`}
              style={{ fontSize: 13, fontWeight: 500, textDecoration: "none", color: "inherit" }}
            >
              {workspace}
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>/</span>
            <span className="topic-breadcrumb-current">{topic}</span>
          </div>
          <div className="row" style={{ gap: 6 }}>
            <span className="status-dot" data-status={connectionStatus} title={connectionStatus} />
            <span className="muted" style={{ fontSize: 12 }}>{connectionStatus}</span>
          </div>
        </div>
        <div className="topic-header-body">
          <div className="topic-controls">
            <section className="topic-control-block">
              <div className="topic-toolbar-label">Participant</div>
              <ParticipantNameInput compact showLabel={false} />
            </section>

            <section className="topic-control-block">
              <div className="topic-toolbar-label">Topic Actions</div>
              <div className="row" style={{ gap: 6 }}>
                <Link href="/ws-language" className="btn btn-secondary btn-sm">
                  WS Reference
                </Link>
                <a href={shelleyHref} className="btn btn-secondary btn-sm" target="_blank" rel="noopener noreferrer">
                  Open in Shelley
                </a>
                <button className="btn btn-danger btn-sm" onClick={handleDeleteTopic}>
                  Delete Topic
                </button>
              </div>
            </section>
          </div>

          <details className="topic-cli-details">
            <summary className="muted" style={{ fontSize: 12, cursor: "pointer" }}>
              Open in CLI
            </summary>
            <pre style={{ marginTop: 8, fontSize: 11 }}>{cliCommand}</pre>
          </details>
        </div>
      </div>

      {/* Messages — fills remaining space, scrolls */}
      <div className="card messages-scroll" style={{ padding: 12 }}>
        <MessageList messages={messages} />
      </div>

      {/* Active turn status bar */}
      {turnActive && (
        <div className="row row-between turn-bar" style={{ flexShrink: 0 }}>
          <span className="muted" style={{ fontSize: 13 }}>
            <span className="status-dot" data-status="running" style={{ marginRight: 6 }} />
            Agent is working{queue.activePromptId ? ` (${queue.activePromptId})` : ""}
          </span>
          <button
            className="btn-stop btn-sm"
            onClick={sendInterrupt}
            disabled={connectionStatus !== "connected"}
          >
            Stop
          </button>
        </div>
      )}

      {/* Queue (only shown when entries exist) */}
      {queue.entries.length > 0 && (
        <div className="card" style={{ flexShrink: 0 }}>
          <QueuePanel />
        </div>
      )}

      {/* Composer — pinned at bottom */}
      <div className="card" style={{ flexShrink: 0 }}>
        <form onSubmit={handleSubmit}>
          <textarea
            ref={textareaRef}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSubmit(e);
              }
            }}
            placeholder="Enter a prompt..."
            style={{ minHeight: 48 }}
          />
          <div className="row" style={{ marginTop: 6, gap: 6 }}>
            <button
              className="btn btn-primary"
              type="submit"
              disabled={connectionStatus !== "connected" || !prompt.trim()}
            >
              Send
            </button>
            {connectionStatus !== "connected" && (
              <span className="muted" style={{ fontSize: 12 }}>
                {connectionStatus === "connecting" ? "Connecting..." : "Disconnected"}
              </span>
            )}
          </div>
        </form>
      </div>
    </div>
  );
}
