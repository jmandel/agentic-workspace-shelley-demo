import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { connectAuthenticatedSocket } from "./socket";

const originalWebSocket = globalThis.WebSocket;
const originalWindow = globalThis.window;
const originalLocalStorage = globalThis.localStorage;

class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;

  readyState = FakeWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  sent: string[] = [];

  constructor(public url: string) {}

  send(payload: string) {
    this.sent.push(payload);
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
  }
}

describe("connectAuthenticatedSocket", () => {
  let intervalCallbacks: Array<() => void>;
  let timeoutCallbacks: Array<() => void>;

  beforeEach(() => {
    intervalCallbacks = [];
    timeoutCallbacks = [];
    const values = new Map<string, string>();
    globalThis.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    globalThis.localStorage = {
      getItem(key: string) {
        return values.has(key) ? values.get(key)! : null;
      },
      setItem(key: string, value: string) {
        values.set(key, value);
      },
      removeItem(key: string) {
        values.delete(key);
      },
      clear() {
        values.clear();
      },
      key(index: number) {
        return Array.from(values.keys())[index] ?? null;
      },
      get length() {
        return values.size;
      },
    } as Storage;
    globalThis.window = {
      setInterval: ((fn: () => void) => {
        intervalCallbacks.push(fn);
        return intervalCallbacks.length;
      }) as typeof window.setInterval,
      clearInterval: mock(() => {}),
      setTimeout: ((fn: () => void) => {
        timeoutCallbacks.push(fn);
        return timeoutCallbacks.length;
      }) as typeof window.setTimeout,
    } as Window & typeof globalThis;
  });

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket;
    globalThis.window = originalWindow;
    globalThis.localStorage = originalLocalStorage;
    mock.restore();
  });

  test("forces reconnect when an active socket goes stale", () => {
    const now = mock(() => 0);
    Date.now = now;

    let activeSocket: WebSocket | null = null;
    let status = "connecting";
    let reconnects = 0;

    const ws = connectAuthenticatedSocket({
      url: "ws://example.test/topic",
      reconnectDelayMs: 250,
      staleTimeoutMs: 30_000,
      getActiveSocket: () => activeSocket,
      setActiveSocket: (socket) => {
        activeSocket = socket;
      },
      setStatus: (nextStatus) => {
        status = nextStatus;
      },
      shouldReconnect: () => true,
      shouldConsiderStale: () => true,
      reconnect: () => {
        reconnects += 1;
      },
      onMessage: () => {},
    });

    expect(activeSocket).toBe(ws);

    const fake = ws as unknown as FakeWebSocket;
    fake.readyState = FakeWebSocket.OPEN;
    fake.onopen?.();

    now.mockImplementation(() => 31_000);
    intervalCallbacks[0]?.();

    expect(activeSocket).toBeNull();
    expect(status).toBe("disconnected");
    expect(fake.readyState).toBe(FakeWebSocket.CLOSED);
    expect(reconnects).toBe(0);

    timeoutCallbacks[0]?.();
    expect(reconnects).toBe(1);
  });

  test("reconnects immediately when the server closes with try-again-later", () => {
    let activeSocket: WebSocket | null = null;
    let reconnects = 0;

    const ws = connectAuthenticatedSocket({
      url: "ws://example.test/topic",
      reconnectDelayMs: 1500,
      getActiveSocket: () => activeSocket,
      setActiveSocket: (socket) => {
        activeSocket = socket;
      },
      setStatus: () => {},
      shouldReconnect: () => true,
      reconnect: () => {
        reconnects += 1;
      },
      onMessage: () => {},
    });

    const fake = ws as unknown as FakeWebSocket;
    fake.onclose?.({ code: 1013 } as CloseEvent);

    expect(reconnects).toBe(0);
    expect(timeoutCallbacks).toHaveLength(1);
    timeoutCallbacks[0]?.();
    expect(reconnects).toBe(1);
  });
});
