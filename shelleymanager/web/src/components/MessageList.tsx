import { useEffect, useRef, useState } from "react";
import type { ChatMessage } from "@/store";

const KIND_CLASS: Record<string, string> = {
  system: "msg-system",
  user: "msg-user",
  assistant: "",
  error: "msg-error",
  tool: "msg-tool",
  interrupted: "msg-interrupted",
};

const COLLAPSE_LINE_THRESHOLD = 4;

function MessageBody({ msg }: { msg: ChatMessage }) {
  const [expanded, setExpanded] = useState(false);
  const collapsible =
    msg.kind === "tool" && msg.body.split("\n").length > COLLAPSE_LINE_THRESHOLD;

  return (
    <>
      <div className={`msg-body ${collapsible && !expanded ? "msg-body-clamped" : ""}`}>
        {msg.body}
      </div>
      {collapsible && (
        <button
          className="msg-toggle"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? "Show less" : "Show more"}
        </button>
      )}
    </>
  );
}

export function MessageList({ messages }: { messages: ChatMessage[] }) {
  const endRef = useRef<HTMLDivElement>(null);
  const rafRef = useRef(0);

  useEffect(() => {
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
            {msg.label}
          </div>
          <MessageBody msg={msg} />
        </div>
      ))}
      <div ref={endRef} />
    </div>
  );
}
