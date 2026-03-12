import { socketAuthenticationMessage } from "./auth";

export type SocketStatus = "connecting" | "connected" | "disconnected";

const IMMEDIATE_RECONNECT_CLOSE_CODES = new Set([1013]);

type SocketEnvelope = {
  type?: string;
};

type ParseMessage<TMessage> = (raw: string) => TMessage;

interface AuthenticatedSocketOptions<TMessage extends SocketEnvelope> {
  url: string;
  reconnectDelayMs: number;
  staleTimeoutMs?: number;
  getActiveSocket: () => WebSocket | null;
  setActiveSocket: (ws: WebSocket | null) => void;
  setStatus: (status: SocketStatus) => void;
  shouldReconnect: () => boolean;
  shouldConsiderStale?: () => boolean;
  reconnect: () => void;
  onConnected?: (message: TMessage) => void;
  onAuthenticated?: (message: TMessage) => void;
  onMessage: (message: TMessage) => void;
  parseMessage?: ParseMessage<TMessage>;
}

export function connectAuthenticatedSocket<TMessage extends SocketEnvelope>({
  url,
  reconnectDelayMs,
  staleTimeoutMs,
  getActiveSocket,
  setActiveSocket,
  setStatus,
  shouldReconnect,
  shouldConsiderStale,
  reconnect,
  onConnected,
  onAuthenticated,
  onMessage,
  parseMessage = defaultParseMessage,
}: AuthenticatedSocketOptions<TMessage>): WebSocket {
  const ws = new WebSocket(url);
  setActiveSocket(ws);
  setStatus("connecting");

  const isActive = () => getActiveSocket() === ws;
  let lastActivityAt = Date.now();
  let staleCheckTimer: number | null = null;

  const clearStaleCheck = () => {
    if (staleCheckTimer !== null) {
      window.clearInterval(staleCheckTimer);
      staleCheckTimer = null;
    }
  };

  const noteActivity = () => {
    lastActivityAt = Date.now();
  };

  const triggerReconnect = () => {
    if (!isActive()) return;
    setActiveSocket(null);
    setStatus("disconnected");
    clearStaleCheck();
    try {
      ws.close();
    } catch {
      // Ignore close errors while forcing a reconnect.
    }
    if (!shouldReconnect()) return;
    window.setTimeout(() => {
      if (getActiveSocket() !== null || !shouldReconnect()) return;
      reconnect();
    }, reconnectDelayMs);
  };

  if (staleTimeoutMs && staleTimeoutMs > 0) {
    const intervalMs = Math.max(1000, Math.min(staleTimeoutMs / 2, 10000));
    staleCheckTimer = window.setInterval(() => {
      if (!isActive()) {
        clearStaleCheck();
        return;
      }
      if (ws.readyState !== WebSocket.OPEN) return;
      if (shouldConsiderStale && !shouldConsiderStale()) {
        noteActivity();
        return;
      }
      if (Date.now() - lastActivityAt < staleTimeoutMs) return;
      triggerReconnect();
    }, intervalMs);
  }

  ws.onopen = () => {
    if (!isActive()) return;
    noteActivity();
    ws.send(socketAuthenticationMessage());
  };

  ws.onclose = (event) => {
    if (!isActive()) return;
    setActiveSocket(null);
    setStatus("disconnected");
    clearStaleCheck();
    if (!shouldReconnect()) return;
    const reconnectDelay = IMMEDIATE_RECONNECT_CLOSE_CODES.has(event.code)
      ? 0
      : reconnectDelayMs;
    window.setTimeout(() => {
      if (getActiveSocket() !== null || !shouldReconnect()) return;
      reconnect();
    }, reconnectDelay);
  };

  ws.onerror = () => {
    if (!isActive()) return;
    if (ws.readyState !== WebSocket.OPEN) {
      setStatus("disconnected");
    }
  };

  ws.onmessage = (event: MessageEvent) => {
    if (!isActive()) return;
    noteActivity();

    let message: TMessage;
    try {
      message = parseMessage(String(event.data));
    } catch {
      return;
    }

    if (message.type === "authenticated") {
      onAuthenticated?.(message);
      return;
    }
    if (message.type === "connected") {
      setStatus("connected");
      onConnected?.(message);
      return;
    }

    onMessage(message);
  };

  return ws;
}

function defaultParseMessage<TMessage>(raw: string): TMessage {
  return JSON.parse(raw) as TMessage;
}
