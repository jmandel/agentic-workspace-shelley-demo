#!/usr/bin/env bun

import { Database } from "bun:sqlite";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

const DEFAULT_DB_PATH = new URL("../data/jira-data.db", import.meta.url).pathname;
const DB_PATH = process.env.HL7_JIRA_DB || DEFAULT_DB_PATH;

function getDb() {
  return new Database(DB_PATH, { readonly: true });
}

function tokenizeQuery(query) {
  return String(query || "")
    .trim()
    .split(/\s+/)
    .map((token) => token.replace(/[^\p{L}\p{N}_-]+/gu, "").trim())
    .filter(Boolean);
}

function buildFTSQuery(query) {
  const tokens = tokenizeQuery(query);
  if (tokens.length === 0) {
    return "";
  }
  return tokens.map((token) => `"${token}"`).join(" AND ");
}

function parseIssueRow(row) {
  if (!row) return null;
  const data = typeof row.data === "string" ? JSON.parse(row.data) : row.data;
  return {
    key: data.key,
    summary: data.summary || "",
    status: data.status || "",
    url: data.url || (data.key ? `https://jira.hl7.org/browse/${data.key}` : ""),
    specification: data.specification || [],
    relatedArtifacts: data.related_artifacts || [],
    workGroup: data.work_group || [],
    updatedAt: data.updated_at || "",
  };
}

function searchIssues(db, query, limit) {
  const ftsQuery = buildFTSQuery(query);
  if (ftsQuery) {
    try {
      const rows = db
        .query(`
          SELECT i.data
          FROM issues_fts fts
          JOIN issues i ON i.rowid = fts.rowid
          WHERE issues_fts MATCH ?1
          ORDER BY fts.rank
          LIMIT ?2
        `)
        .all(ftsQuery, limit);
      if (rows.length > 0) {
        return rows.map(parseIssueRow).filter(Boolean);
      }
    } catch {
      // Fall back to broad LIKE matching below if the FTS parser rejects the query.
    }
  }

  const like = `%${String(query || "").trim()}%`;
  const rows = db
    .query(`
      SELECT data
      FROM issues
      WHERE json_extract(data, '$.summary') LIKE ?1
         OR json_extract(data, '$.description') LIKE ?1
         OR json_extract(data, '$.comments_text') LIKE ?1
         OR key LIKE ?1
      ORDER BY json_extract(data, '$.updated_at') DESC
      LIMIT ?2
    `)
    .all(like, limit);
  return rows.map(parseIssueRow).filter(Boolean);
}

function formatList(results) {
  if (results.length === 0) {
    return "No matching HL7 Jira issues found.";
  }
  return results
    .map((issue) => {
      const artifacts = Array.isArray(issue.relatedArtifacts) ? issue.relatedArtifacts.filter(Boolean).slice(0, 3) : [];
      const workGroup = Array.isArray(issue.workGroup) ? issue.workGroup.filter(Boolean).slice(0, 2) : [];
      const meta = [];
      if (issue.status) meta.push(`status: ${issue.status}`);
      if (artifacts.length > 0) meta.push(`artifacts: ${artifacts.join(", ")}`);
      if (workGroup.length > 0) meta.push(`wg: ${workGroup.join(", ")}`);
      const suffix = meta.length > 0 ? ` [${meta.join(" | ")}]` : "";
      return `- ${issue.key}: ${issue.summary}${suffix} (${issue.url})`;
    })
    .join("\n");
}

async function main() {
  const db = getDb();
  const server = new McpServer({
    name: "hl7-jira-search",
    version: "0.2.0"
  });

  server.tool(
    "jira.search",
    "Search the real HL7 Jira SQLite snapshot from the fhir-community-search project.",
    {
      query: z.string().min(1).describe("Free-text query for HL7 Jira issues"),
      limit: z.number().int().min(1).max(10).optional().describe("Maximum number of issues to return")
    },
    async ({ query, limit }) => {
      const results = searchIssues(db, query, limit ?? 5);
      return {
        content: [
          {
            type: "text",
            text: formatList(results)
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
