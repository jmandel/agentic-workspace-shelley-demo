import { afterEach, beforeEach, describe, expect, test } from "bun:test";

const originalLocalStorage = globalThis.localStorage;

function installLocalStorage() {
  const values = new Map<string, string>();
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
}

describe("topic store", () => {
  beforeEach(() => {
    installLocalStorage();
  });

  afterEach(() => {
    globalThis.localStorage = originalLocalStorage;
  });

  test("sendPrompt allows another prompt while a turn is already active", async () => {
    const { useStore } = await import("./store");

    const sent: string[] = [];
    const fakeWs = {
      readyState: WebSocket.OPEN,
      send(payload: string) {
        sent.push(payload);
      },
    } as unknown as WebSocket;

    useStore.setState({
      _ws: fakeWs,
      activeRun: null,
      turnActive: false,
      _pendingPromptTexts: [],
    });

    useStore.setState({
      activeRun: { runId: "r_active", state: "running" },
      turnActive: true,
    });

    expect(useStore.getState().sendPrompt("queued prompt")).toBeTrue();
    expect(sent).toHaveLength(1);
    expect(JSON.parse(sent[0] ?? "{}")).toEqual({
      type: "prompt",
      data: "queued prompt",
    });
    expect(useStore.getState()._pendingPromptTexts).toEqual(["queued prompt"]);
    expect(useStore.getState().turnActive).toBeTrue();
  });
});
