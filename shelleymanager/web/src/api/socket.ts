import { socketAuthenticationMessage } from "./auth";

export type SocketStatus = "connecting" | "connected" | "disconnected";

type SocketEnvelope = {
  type?: string;
};

type ParseMessage<TMessage> = (raw: string) => TMessage;

interface AuthenticatedSocketOptions<TMessage extends SocketEnvelope> {
  url: string;
  reconnectDelayMs: number;
  getActiveSocket: () => WebSocket | null;
  setActiveSocket: (ws: WebSocket | null) => void;
  setStatus: (status: SocketStatus) => void;
  shouldReconnect: () => boolean;
  reconnect: () => void;
  onConnected?: (message: TMessage) => void;
  onAuthenticated?: (message: TMessage) => void;
  onMessage: (message: TMessage) => void;
  parseMessage?: ParseMessage<TMessage>;
}

export function connectAuthenticatedSocket<TMessage extends SocketEnvelope>({
  url,
  reconnectDelayMs,
  getActiveSocket,
  setActiveSocket,
  setStatus,
  shouldReconnect,
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

  ws.onopen = () => {
    if (!isActive()) return;
    ws.send(socketAuthenticationMessage());
  };

  ws.onclose = () => {
    if (!isActive()) return;
    setActiveSocket(null);
    setStatus("disconnected");
    if (!shouldReconnect()) return;
    window.setTimeout(() => {
      if (getActiveSocket() !== null || !shouldReconnect()) return;
      reconnect();
    }, reconnectDelayMs);
  };

  ws.onerror = () => {
    if (!isActive()) return;
    if (ws.readyState !== WebSocket.OPEN) {
      setStatus("disconnected");
    }
  };

  ws.onmessage = (event: MessageEvent) => {
    if (!isActive()) return;

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
