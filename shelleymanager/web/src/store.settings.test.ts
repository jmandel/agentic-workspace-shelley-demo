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

describe("settings store", () => {
  beforeEach(() => {
    installLocalStorage();
  });

  afterEach(() => {
    globalThis.localStorage = originalLocalStorage;
  });

  test("opens with the current name and saves a normalized update", async () => {
    const { useStore } = await import("./store");

    useStore.setState({
      participantName: "Cedar Harbor 12345",
      participantSubject: "web-test",
      settingsOpen: false,
      settingsDraftName: "",
      _eventsWs: null,
      topicConnection: null,
    });

    useStore.getState().openSettings();
    expect(useStore.getState().settingsOpen).toBeTrue();
    expect(useStore.getState().settingsDraftName).toBe("Cedar Harbor 12345");

    useStore.getState().setSettingsDraftName("  river   pilot  ");
    useStore.getState().saveSettings();

    expect(useStore.getState().settingsOpen).toBeFalse();
    expect(useStore.getState().participantName).toBe("river pilot");
    expect(useStore.getState().settingsDraftName).toBe("river pilot");
  });

  test("randomizeSettingsDraftName generates an adjective noun number name", async () => {
    const { useStore } = await import("./store");

    useStore.setState({
      participantName: "Cedar Harbor 12345",
      participantSubject: "web-test",
      settingsOpen: true,
      settingsDraftName: "Cedar Harbor 12345",
      _eventsWs: null,
      topicConnection: null,
    });

    useStore.getState().randomizeSettingsDraftName();

    expect(useStore.getState().settingsDraftName).toMatch(
      /^[A-Z][a-z]+ [A-Z][a-z]+ \d{5}$/,
    );
  });
});
