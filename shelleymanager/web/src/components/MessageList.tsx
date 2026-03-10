import { useEffect, useRef } from "react";
import type { ChatMessage } from "@/store";

const KIND_CLASS: Record<string, string> = {
  system: "msg-system",
  user: "msg-user",
  assistant: "",
  error: "msg-error",
  tool: "msg-tool",
  injected: "msg-injected",
  interrupted: "msg-interrupted",
};

export function MessageList({ messages }: { messages: ChatMessage[] }) {
  const endRef = useRef<HTMLDivElement>(null);
  const rafRef = useRef(0);

  useEffect(() => {
    // Coalesce rapid updates (e.g. history replay) into a single scroll
    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      endRef.current?.scrollIntoView();
    });
  }, [messages.length]);

  if (messages.length === 0) {
    return (
      <div className="muted" style={{ padding: "16px 0", textAlign: "center", fontSize: 13 }}>
        No messages yet.
      </div>
    );
  }

  return (
    <div className="stack-sm">
      {messages.map((msg) => (
        <div key={msg.id} className={`msg ${KIND_CLASS[msg.kind] ?? ""}`}>
          <div className="msg-label">
            {msg.kind === "injected" && "Injected \u00b7 "}
            {msg.label}
          </div>
          <div className="msg-body">{msg.body}</div>
        </div>
      ))}
      <div ref={endRef} />
    </div>
  );
}
