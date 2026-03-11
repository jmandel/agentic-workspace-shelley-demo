import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import {
  fetchWorkspaceFiles,
  readWorkspaceFileContent,
  uploadWorkspaceFile,
} from "./client";

const originalFetch = globalThis.fetch;
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

describe("workspace file client", () => {
  beforeEach(() => {
    installLocalStorage();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    globalThis.localStorage = originalLocalStorage;
  });

  test("lists workspace files through the new metadata endpoint", async () => {
    let seenURL = "";
    let seenAuth = "";
    globalThis.fetch = (async (input, init) => {
      seenURL = String(input);
      seenAuth = String((init?.headers as Record<string, string>)?.Authorization);
      return new Response(
        JSON.stringify({
          node: {
            path: "docs",
            name: "docs",
            kind: "directory",
            size: 0,
            modifiedAt: "2026-03-11T00:00:00Z",
          },
          entries: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }) as typeof fetch;

    const result = await fetchWorkspaceFiles("acme", "demo", "docs");
    expect(seenURL).toBe("/apis/v1/namespaces/acme/workspaces/demo/files?path=docs");
    expect(seenAuth.startsWith("Bearer ")).toBeTrue();
    expect(result.node.path).toBe("docs");
  });

  test("reads raw workspace file content", async () => {
    let seenURL = "";
    globalThis.fetch = (async (input) => {
      seenURL = String(input);
      return new Response("hello workspace", {
        status: 200,
        headers: { "Content-Type": "text/plain" },
      });
    }) as typeof fetch;

    const response = await readWorkspaceFileContent("acme", "demo", "docs/note.txt");
    expect(seenURL).toBe(
      "/apis/v1/namespaces/acme/workspaces/demo/files/content?path=docs%2Fnote.txt",
    );
    expect(await response.text()).toBe("hello workspace");
  });

  test("uploads file bodies to the content endpoint", async () => {
    let seenURL = "";
    let seenMethod = "";
    let seenBody: BodyInit | null | undefined;
    globalThis.fetch = (async (input, init) => {
      seenURL = String(input);
      seenMethod = String(init?.method);
      seenBody = init?.body;
      return new Response(
        JSON.stringify({
          node: {
            path: "docs/note.txt",
            name: "note.txt",
            kind: "file",
            size: 15,
            modifiedAt: "2026-03-11T00:00:00Z",
            mimeType: "text/plain",
          },
        }),
        { status: 201, headers: { "Content-Type": "application/json" } },
      );
    }) as typeof fetch;

    const file = new File(["hello workspace"], "note.txt", {
      type: "text/plain",
    });
    const result = await uploadWorkspaceFile("acme", "demo", "docs/note.txt", file);
    expect(seenURL).toBe(
      "/apis/v1/namespaces/acme/workspaces/demo/files/content?path=docs%2Fnote.txt",
    );
    expect(seenMethod).toBe("PUT");
    expect(seenBody).toBe(file);
    expect(result.node.path).toBe("docs/note.txt");
  });
});
