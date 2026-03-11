import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import {
  generateParticipantDisplayName,
  loadClientIdentity,
  updateClientDisplayName,
} from "./auth";

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

describe("auth identity helpers", () => {
  beforeEach(() => {
    installLocalStorage();
  });

  afterEach(() => {
    globalThis.localStorage = originalLocalStorage;
  });

  test("generates adjective noun number display names", () => {
    const sequence = [0, 0, 0];
    let index = 0;
    const name = generateParticipantDisplayName(() => sequence[index++] ?? 0);
    expect(name).toBe("Amber Badger 10000");
  });

  test("loads a generated display name instead of falling back to the subject", () => {
    const identity = loadClientIdentity();
    expect(identity.subject.startsWith("web-")).toBeTrue();
    expect(identity.displayName).toMatch(/^[A-Z][a-z]+ [A-Z][a-z]+ \d{5}$/);
    expect(identity.displayName).not.toBe(identity.subject);
  });

  test("normalizes updated display names", () => {
    loadClientIdentity();
    const updated = updateClientDisplayName("  river   pilot  ");
    expect(updated.displayName).toBe("river pilot");
  });

  test("keeps the current display name when a blank update is submitted", () => {
    const identity = loadClientIdentity();
    const updated = updateClientDisplayName("   ");
    expect(updated.displayName).toBe(identity.displayName);
  });
});
