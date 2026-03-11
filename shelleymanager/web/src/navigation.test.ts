import { describe, expect, test } from "bun:test";
import {
  topicPageHref,
  workspaceFilesPageHref,
  workspacePageHref,
} from "./navigation";

describe("navigation helpers", () => {
  test("build workspace and topic hrefs", () => {
    expect(workspacePageHref("acme", "demo")).toBe("/app/acme/demo");
    expect(topicPageHref("acme", "demo", "general")).toBe(
      "/app/acme/demo/general",
    );
  });

  test("builds a dedicated workspace files href", () => {
    expect(workspaceFilesPageHref("acme", "demo")).toBe("/app/acme/demo/files");
    expect(workspaceFilesPageHref("acme space", "demo/work")).toBe(
      "/app/acme%20space/demo%2Fwork/files",
    );
  });
});
