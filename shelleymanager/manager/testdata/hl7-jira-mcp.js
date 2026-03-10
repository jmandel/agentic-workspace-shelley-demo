#!/usr/bin/env bun

import { Database } from "bun:sqlite";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

const issueSeeds = [
  {
    key: "FHIR-53953",
    summary: "No documentation on remote interactions - timeouts, error handling, caching, performance etc.",
    snippet: "Request for clearer guidance on timeout behavior and validation-time error handling for remote terminology and profile interactions.",
    url: "https://jira.hl7.org/browse/FHIR-53953"
  },
  {
    key: "FHIR-53960",
    summary: "Additional Functions - Inconsistent error handling patterns",
    snippet: "Discussion of inconsistent return-empty versus throw-error behavior across functions used in validation contexts.",
    url: "https://jira.hl7.org/browse/FHIR-53960"
  },
  {
    key: "FHIR-51091",
    summary: "ElementDefinition contexts table is incomplete",
    snippet: "Notes validator friction when interpreting constraints like minValue, maxValue, and maxLength in logical models.",
    url: "https://jira.hl7.org/browse/FHIR-51091"
  },
  {
    key: "FHIR-31709",
    summary: "Whole-system history should support alternate sort order",
    snippet: "Change proposal related to synchronization, ordering, and server-side behavior for clients processing recent updates.",
    url: "https://jira.hl7.org/browse/FHIR-31709"
  }
];

function ensureDemoDB() {
  const workspaceRoot = process.cwd();
  const demoDir = join(workspaceRoot, ".demo");
  mkdirSync(demoDir, { recursive: true });
  const db = new Database(join(demoDir, "hl7-community-search.sqlite"), { create: true });
  db.exec(`
    CREATE TABLE IF NOT EXISTS jira_issues (
      issue_key TEXT PRIMARY KEY,
      summary TEXT NOT NULL,
      snippet TEXT NOT NULL,
      url TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS jira_issues_search_idx
      ON jira_issues(summary, snippet);
  `);

  const row = db.query("SELECT COUNT(*) AS count FROM jira_issues").get();
  const count = typeof row?.count === "number" ? row.count : Number(row?.count || 0);
  if (count === 0) {
    const insert = db.query(`
      INSERT INTO jira_issues (issue_key, summary, snippet, url)
      VALUES (?, ?, ?, ?)
    `);
    for (const issue of issueSeeds) {
      insert.run(issue.key, issue.summary, issue.snippet, issue.url);
    }
  }

  return db;
}

function tokenize(query) {
  return String(query || "")
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .map((token) => token.trim())
    .filter(Boolean);
}

function searchIssues(db, query) {
  const rows = db
    .query(`
      SELECT issue_key AS issueKey, summary, snippet, url
      FROM jira_issues
    `)
    .all();

  const tokens = tokenize(query);
  if (tokens.length === 0) {
    return rows.slice(0, 3);
  }

  const ranked = rows
    .map((row) => {
      const haystack = [row.issueKey, row.summary, row.snippet].join(" ").toLowerCase();
      let score = 0;
      for (const token of tokens) {
        if (haystack.includes(token)) {
          score += 1;
        }
      }
      return { row, score };
    })
    .filter((entry) => entry.score > 0)
    .sort((left, right) => {
      if (right.score !== left.score) {
        return right.score - left.score;
      }
      return left.row.issueKey.localeCompare(right.row.issueKey);
    })
    .map((entry) => entry.row);

  return ranked.length > 0 ? ranked.slice(0, 3) : rows.slice(0, 3);
}

function formatResults(query, rows) {
  if (rows.length === 0) {
    return `No HL7 Jira fixture matches for query: ${query}`;
  }
  return rows
    .map((row) => `- ${row.issueKey}: ${row.summary} — ${row.snippet} (${row.url})`)
    .join("\n");
}

async function main() {
  const db = ensureDemoDB();
  const server = new McpServer({
    name: "hl7-jira-demo",
    version: "0.1.0"
  });

  server.tool(
    "jira.search",
    "Search a small HL7 Jira fixture derived from fhir-community-search examples.",
    {
      query: z.string().min(1).describe("Free-text search for a validator or FHIRPath issue")
    },
    async ({ query }) => {
      const results = searchIssues(db, query);
      return {
        content: [
          {
            type: "text",
            text: formatResults(query, results)
          }
        ],
        structuredContent: {
          query,
          results
        }
      };
    }
  );

  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
