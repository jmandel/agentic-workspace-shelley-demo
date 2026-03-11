import { describe, expect, test } from "bun:test";
import {
  topicFilesPageHref,
  topicPageHref,
  workspacePageHref,
} from "./navigation";

describe("navigation helpers", () => {
  test("build workspace and topic hrefs", () => {
    expect(workspacePageHref("acme", "demo")).toBe("/app/acme/demo");
    expect(topicPageHref("acme", "demo", "general")).toBe(
      "/app/acme/demo/general",
    );
  });

  test("builds a dedicated topic files href", () => {
    expect(topicFilesPageHref("acme", "demo", "general")).toBe(
      "/app/acme/demo/general/files",
    );
    expect(topicFilesPageHref("acme space", "demo/work", "topic name")).toBe(
      "/app/acme%20space/demo%2Fwork/topic%20name/files",
    );
  });
});
