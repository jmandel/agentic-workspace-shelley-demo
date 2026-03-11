import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, test } from "bun:test";
import { MessageList } from "./MessageList";

test("MessageList renders tool text faithfully and safely", () => {
  const html = renderToStaticMarkup(
    createElement(MessageList, {
      messages: [
        {
          id: "m1",
          kind: "tool",
          label: "Bash",
          body: "cat > /tmp/example <tag>",
          ts: Date.now(),
        },
      ],
    }),
  );

  expect(html).toContain("cat &gt; /tmp/example &lt;tag&gt;");
  expect(html).not.toContain("<tag>");
});
