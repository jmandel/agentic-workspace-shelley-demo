import { useEffect, useState, useRef } from "react";
import { Link, useParams, useLocation } from "wouter";
import { useStore } from "@/store";
import { MessageList } from "@/components/MessageList";
import { QueuePanel } from "@/components/QueuePanel";
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
        <div className="row row-between">
          <div className="row" style={{ gap: 6 }}>
            <Link href="/" style={{ color: "var(--muted)", textDecoration: "none", fontSize: 13 }}>
              Workspaces
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>/</span>
            <span style={{ fontSize: 13, fontWeight: 500 }}>{workspace}</span>
            <span className="muted" style={{ fontSize: 13 }}>/</span>
            <span style={{ fontSize: 13, fontWeight: 500 }}>{topic}</span>
          </div>
          <div className="row" style={{ gap: 6 }}>
            <span className="status-dot" data-status={connectionStatus} title={connectionStatus} />
            <span className="muted" style={{ fontSize: 12 }}>{connectionStatus}</span>
          </div>
        </div>
        <div className="row" style={{ marginTop: 6, gap: 6 }}>
          <Link href="/ws-language" className="btn btn-secondary btn-sm">
            WS Reference
          </Link>
          <button className="btn btn-danger btn-sm" onClick={handleDeleteTopic}>
            Delete Topic
          </button>
        </div>
        <details style={{ marginTop: 6 }}>
          <summary className="muted" style={{ fontSize: 12, cursor: "pointer" }}>
            Open in CLI
          </summary>
          <pre style={{ marginTop: 4, fontSize: 11 }}>{cliCommand}</pre>
        </details>
        <details style={{ marginTop: 4 }}>
          <summary className="muted" style={{ fontSize: 12, cursor: "pointer" }}>
            Open in Shelley
          </summary>
          <div style={{ marginTop: 4 }}>
            <a href={shelleyHref} className="btn btn-primary btn-sm">
              Open in Shelley
            </a>
          </div>
        </details>
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
