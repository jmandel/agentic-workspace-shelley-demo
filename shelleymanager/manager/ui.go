package manager

import (
	"html/template"
	"net/http"
	"strings"
)

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Shelley Manager</title>
  <style>
    :root { color-scheme: light; --bg:#f4f0e8; --ink:#1f2430; --muted:#6b7280; --card:#fffdf8; --line:#d7cec0; --accent:#0f766e; --accent-ink:#ffffff; }
    body { margin:0; font: 16px/1.4 Georgia, serif; background: radial-gradient(circle at top left, #f8f4eb, #efe6d8 60%, #e7ddcf); color:var(--ink); }
    main { max-width: 1040px; margin: 0 auto; padding: 32px 20px 64px; }
    h1,h2,h3 { margin:0 0 12px; font-family: "Iowan Old Style", Georgia, serif; }
    .grid { display:grid; grid-template-columns: 1.1fr .9fr; gap:20px; align-items:start; }
    .card { background: var(--card); border: 1px solid var(--line); border-radius: 16px; padding: 18px; box-shadow: 0 12px 40px rgba(31,36,48,.08); }
    label { display:block; margin: 12px 0 6px; font-weight:600; }
    input, textarea { width:100%; box-sizing:border-box; padding:10px 12px; border-radius:10px; border:1px solid var(--line); background:#fff; font: inherit; }
    button { border:0; border-radius: 999px; background: var(--accent); color: var(--accent-ink); padding: 10px 16px; font: inherit; cursor:pointer; }
    button.secondary { background:#ece4d6; color:var(--ink); }
    .row { display:flex; gap:10px; flex-wrap:wrap; align-items:center; }
    .muted { color: var(--muted); }
    .tools { display:grid; gap:10px; }
    .tool { padding:12px; border:1px solid var(--line); border-radius:12px; background:#fff; }
    .workspace-list { display:grid; gap:16px; }
    .workspace-card { padding:16px; border:1px solid var(--line); border-radius:14px; background:#fff; display:grid; gap:14px; }
    .workspace-head { display:flex; justify-content:space-between; gap:14px; align-items:flex-start; }
    .workspace-meta { display:grid; gap:6px; }
    .topics-list { display:grid; gap:12px; }
    .topic-item { padding:12px; border:1px solid #ece4d6; border-radius:12px; background:#f7f2e8; display:grid; gap:10px; }
    .topic-item .row { justify-content:space-between; }
    .topic-actions { justify-content:flex-start; }
    .workspace-create-topic { padding-top:12px; border-top:1px solid #eee4d6; display:grid; gap:10px; }
    .action-link { display:inline-flex; align-items:center; justify-content:center; border-radius:999px; padding:10px 16px; text-decoration:none; }
    .action-link.primary { background:var(--accent); color:var(--accent-ink); }
    .action-link.secondary { background:#ece4d6; color:var(--ink); }
    .toolbar { display:flex; gap:12px; align-items:flex-end; flex-wrap:wrap; margin-top:16px; }
    .toolbar .field { flex:1 1 280px; }
    .inline-input { max-width:320px; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
    pre { white-space: pre-wrap; background: #f7f2e8; border-radius: 12px; padding: 12px; overflow:auto; }
    .status { min-height: 22px; color: var(--muted); }
    @media (max-width: 860px) { .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body data-namespace="{{.Namespace}}">
  <main>
    <div class="card" style="margin-bottom:20px;">
      <h1>Shelley Manager Demo</h1>
      <p class="muted">Create a workspace from the manager-published local tool catalog, then optionally pre-register the HL7 Jira MCP tool with the real workspace APIs.</p>
      <div class="toolbar">
        <div class="field">
          <label for="participant-name" style="margin-top:0;">Participant Name</label>
          <input id="participant-name" class="inline-input" autocomplete="name" placeholder="Priya Shah">
          <div class="muted">Used as your visible name in the topic queue and browser topic session.</div>
        </div>
        <div class="row">
          <button id="save-participant" type="button" class="secondary">Use Name</button>
          <a class="action-link secondary" href="/ws-language">WS Language Tutorial</a>
        </div>
      </div>
      <p id="participant-status" class="status" style="margin:12px 0 0;"></p>
    </div>
    <div class="grid">
      <section class="card">
        <h2>Create Workspace</h2>
        <form id="create-form">
          <label for="workspace-name">Workspace Name</label>
          <input id="workspace-name" name="name" value="bp-ig-fix" required>

          <label for="topic-name">Initial Topic</label>
          <input id="topic-name" name="topic" value="bp-panel-validator" required>

          <label for="template">Template / Repo Label</label>
          <input id="template" name="template" value="acme-rpm-ig">

          <label>Trusted Local Tools</label>
          <div id="local-tools" class="tools"></div>

          <label for="jira-enabled">Managed MCP Tools</label>
          <div class="tool">
            <label class="row" style="margin:0;">
              <input id="jira-enabled" type="checkbox" checked style="width:auto;">
              <span>Register <code>hl7-jira</code> via the workspace tools API</span>
            </label>
            <p class="muted" style="margin:8px 0 0;">This writes a Bun MCP fixture into the workspace, then registers it through the real tools API.</p>
          </div>

          <div class="row" style="margin-top:16px;">
            <button type="submit">Create Workspace</button>
            <button class="secondary" type="button" id="refresh-workspaces">Refresh</button>
          </div>
          <p id="status" class="status"></p>
        </form>
      </section>

      <section class="card">
        <h2>Live Workspaces</h2>
        <div id="workspaces" class="workspace-list muted">Loading…</div>
      </section>
    </div>
  </main>
  <script>
    const namespace = document.body.dataset.namespace;
    const apiBase = '/apis/v1/namespaces/' + encodeURIComponent(namespace) + '/workspaces';
    const localToolsEl = document.getElementById('local-tools');
    const workspacesEl = document.getElementById('workspaces');
    const statusEl = document.getElementById('status');
    const participantNameEl = document.getElementById('participant-name');
    const participantStatusEl = document.getElementById('participant-status');
    const participantKey = 'workspace-participant-id';

    function escapeHTML(value) {
      return String(value == null ? '' : value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function workspaceAPI(ns, name) {
      return '/apis/v1/namespaces/' + encodeURIComponent(ns) + '/workspaces/' + encodeURIComponent(name);
    }

    function randomParticipantName() {
      return 'web-' + Math.random().toString(36).slice(2, 8);
    }

    function normalizeParticipantName(value) {
      return String(value == null ? '' : value).trim().replace(/\s+/g, ' ').slice(0, 64);
    }

    function currentParticipantName() {
      let value = normalizeParticipantName(localStorage.getItem(participantKey) || '');
      if (!value) {
        value = randomParticipantName();
        localStorage.setItem(participantKey, value);
      }
      return value;
    }

    async function readJSON(res, label) {
      const text = await res.text();
      if (!res.ok) {
        const message = text.trim() || ('HTTP ' + res.status);
        throw new Error(label + ': ' + res.status + ' ' + message);
      }
      try {
        return text ? JSON.parse(text) : null;
      } catch (err) {
        throw new Error(label + ': invalid JSON response: ' + text.slice(0, 120));
      }
    }

    async function jsonRequest(url, options, label) {
      const res = await fetch(url, options);
      return readJSON(res, label);
    }

    async function loadLocalTools() {
      const res = await fetch('/apis/v1/local-tools');
      const tools = await readJSON(res, 'load local tools');
      if (!Array.isArray(tools) || tools.length === 0) {
        localToolsEl.innerHTML = '<p class="muted">No local tools published by this manager.</p>';
        return;
      }
      localToolsEl.innerHTML = tools.map((tool, idx) => {
        const requires = (tool.requirements && tool.requirements.length)
          ? '<div class="muted">Requires: ' + tool.requirements.join(', ') + '</div>' : '';
        const commands = (tool.commands && tool.commands.length)
          ? '<div class="muted">Commands: ' + tool.commands.map(c => '<code>' + escapeHTML(c.name) + '</code>').join(', ') + '</div>' : '';
        const checked = idx === 0 ? 'checked' : '';
        return '<label class="tool">'
          + '<div class="row">'
          + '<input type="checkbox" name="localTool" value="' + escapeHTML(tool.name) + '" ' + checked + ' style="width:auto;">'
          + '<strong>' + escapeHTML(tool.name) + '</strong>'
          + '</div>'
          + '<div class="muted">' + escapeHTML(tool.description || '') + '</div>'
          + requires
          + commands
          + '</label>';
      }).join('');
    }

    function loadParticipantName() {
      participantNameEl.value = currentParticipantName();
      participantStatusEl.textContent = 'Current participant: ' + participantNameEl.value;
    }

    function workspaceCard(ws) {
      const wsNamespace = ws.namespace || namespace;
      const topics = Array.isArray(ws.topics) ? ws.topics : [];
      const defaultTopic = (topics[0] && topics[0].name) || 'bp-panel-validator';
      const cli = 'WS_MANAGER=' + window.location.origin + ' bun run cli.ts connect ' + ws.name + ' ' + defaultTopic;
      const localTools = ws.runtime && ws.runtime.localTools ? ws.runtime.localTools.map(t => '<code>' + escapeHTML(t.name) + '</code>').join(', ') : '<span class="muted">none</span>';
      const topicList = topics.length > 0
        ? topics.map(topic => {
            const topicName = topic.name;
            const openHref = '/app/' + encodeURIComponent(wsNamespace) + '/' + encodeURIComponent(ws.name) + '/' + encodeURIComponent(topicName);
            const shelleyHref = '/shelley/' + encodeURIComponent(wsNamespace) + '/' + encodeURIComponent(ws.name) + '/' + encodeURIComponent(topicName);
            return '<div class="topic-item">'
              + '<div class="row">'
              + '<strong><code>' + escapeHTML(topicName) + '</code></strong>'
              + '<button type="button" class="secondary" data-action="delete-topic" data-namespace="' + escapeHTML(wsNamespace) + '" data-workspace="' + escapeHTML(ws.name) + '" data-topic="' + escapeHTML(topicName) + '">Delete Topic</button>'
              + '</div>'
              + '<div class="row topic-actions">'
              + '<a class="action-link primary" href="' + openHref + '">Open Topic</a>'
              + '<a class="action-link secondary" href="' + shelleyHref + '">Open Shelley UI</a>'
              + '</div>'
              + '</div>';
          }).join('')
        : '<p class="muted">No topics yet.</p>';
      return '<section class="workspace-card">'
        + '<div class="workspace-head">'
        + '<div class="workspace-meta">'
        + '<h3>' + escapeHTML(ws.name) + '</h3>'
        + '<div class="muted">Status: ' + escapeHTML(ws.status) + '</div>'
        + '<div class="muted">Local tools: ' + localTools + '</div>'
        + '</div>'
        + '<button type="button" class="secondary" data-action="delete-workspace" data-namespace="' + escapeHTML(wsNamespace) + '" data-workspace="' + escapeHTML(ws.name) + '">Delete Workspace</button>'
        + '</div>'
        + '<div>'
        + '<div class="muted">Topics</div>'
        + '<div class="topics-list">' + topicList + '</div>'
        + '</div>'
        + '<div class="workspace-create-topic">'
        + '<div class="muted">Create Topic</div>'
        + '<div class="row">'
        + '<input type="text" data-role="new-topic-name" data-namespace="' + escapeHTML(wsNamespace) + '" data-workspace="' + escapeHTML(ws.name) + '" placeholder="new-topic-name">'
        + '<button type="button" data-action="create-topic" data-namespace="' + escapeHTML(wsNamespace) + '" data-workspace="' + escapeHTML(ws.name) + '">Create Topic</button>'
        + '</div>'
        + '</div>'
        + '<div>'
        + '<div class="muted">CLI Join</div>'
        + '<pre>' + escapeHTML(cli) + '</pre>'
        + '</div>'
        + '</section>';
    }

    async function loadWorkspaces() {
      const res = await fetch(apiBase);
      const workspaces = await readJSON(res, 'load workspaces');
      if (!Array.isArray(workspaces) || workspaces.length === 0) {
        workspacesEl.innerHTML = '<p class="muted">No workspaces yet.</p>';
        return;
      }
      const details = await Promise.all(workspaces.map(async ws => {
        try {
          const detailRes = await fetch(apiBase + '/' + encodeURIComponent(ws.name));
          return await readJSON(detailRes, 'load workspace detail for ' + ws.name);
        } catch (err) {
          return ws;
        }
      }));
      workspacesEl.innerHTML = details.map(workspaceCard).join('');
    }

    workspacesEl.addEventListener('click', async (event) => {
      const button = event.target.closest('button[data-action]');
      if (!button) return;

      const action = button.dataset.action;
      const wsNamespace = button.dataset.namespace || namespace;
      const wsName = button.dataset.workspace;
      if (!wsName) return;

      try {
        if (action === 'create-topic') {
          const input = workspacesEl.querySelector('input[data-role="new-topic-name"][data-namespace="' + CSS.escape(wsNamespace) + '"][data-workspace="' + CSS.escape(wsName) + '"]');
          const topicName = input && input.value ? input.value.trim() : '';
          if (!topicName) {
            statusEl.textContent = 'Enter a topic name first.';
            return;
          }
          statusEl.textContent = 'Creating topic...';
          await jsonRequest(workspaceAPI(wsNamespace, wsName) + '/topics', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({name: topicName})
          }, 'create topic');
          window.location.href = '/app/' + encodeURIComponent(wsNamespace) + '/' + encodeURIComponent(wsName) + '/' + encodeURIComponent(topicName);
          return;
        }

        if (action === 'delete-topic') {
          const topicName = button.dataset.topic;
          if (!topicName) return;
          if (!window.confirm('Delete topic "' + topicName + '"?')) return;
          statusEl.textContent = 'Deleting topic...';
          const res = await fetch(workspaceAPI(wsNamespace, wsName) + '/topics/' + encodeURIComponent(topicName), {
            method: 'DELETE'
          });
          if (!res.ok) {
            throw new Error('delete topic: ' + res.status + ' ' + (await res.text()));
          }
          await loadWorkspaces();
          statusEl.textContent = '';
          return;
        }

        if (action === 'delete-workspace') {
          if (!window.confirm('Delete workspace "' + wsName + '"?')) return;
          statusEl.textContent = 'Deleting workspace...';
          const res = await fetch(workspaceAPI(wsNamespace, wsName), {method: 'DELETE'});
          if (!res.ok) {
            throw new Error('delete workspace: ' + res.status + ' ' + (await res.text()));
          }
          await loadWorkspaces();
          statusEl.textContent = '';
        }
      } catch (err) {
        statusEl.textContent = err.message || String(err);
      }
    });

    async function registerDemoJiraTool(name) {
      const fixtureAssetRes = await fetch('/demo-assets/hl7-jira-mcp.js');
      const jiraFixtureScript = await fixtureAssetRes.text();
      if (!fixtureAssetRes.ok) {
        throw new Error('load hl7-jira fixture: ' + fixtureAssetRes.status + ' ' + (jiraFixtureScript.trim() || ''));
      }

      const fixtureRes = await fetch(apiBase + '/' + encodeURIComponent(name) + '/files/.demo/hl7-jira-mcp.js', {
        method: 'PUT',
        headers: {'Content-Type': 'text/plain; charset=utf-8'},
        body: jiraFixtureScript
      });
      if (!fixtureRes.ok) {
        throw new Error('write hl7-jira fixture: ' + fixtureRes.status);
      }

      const createRes = await fetch(apiBase + '/' + encodeURIComponent(name) + '/tools', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
          name: 'hl7-jira',
          description: 'Search realistic HL7 Jira fixture data',
          provider: 'demo@acme.example',
          protocol: 'mcp',
          transport: {
            type: 'stdio',
            command: 'bun',
            args: ['./.demo/hl7-jira-mcp.js'],
            cwd: '.'
          },
          tools: [{
            name: 'jira.search',
            title: 'Search HL7 Jira',
            description: 'Search realistic HL7 Jira issues related to validation and FHIRPath behavior',
            inputSchema: {
              type: 'object',
              properties: { query: { type: 'string' } },
              required: ['query'],
              additionalProperties: false
            }
          }]
        })
      });
      if (!createRes.ok && createRes.status !== 409) {
        throw new Error('register hl7-jira: ' + createRes.status);
      }
      const grantRes = await fetch(apiBase + '/' + encodeURIComponent(name) + '/tools/hl7-jira/grants', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
          subject: 'agent:*',
          tools: ['jira.search'],
          access: 'allowed',
          approvers: [],
          scope: {}
        })
      });
      if (!grantRes.ok && grantRes.status !== 409) {
        throw new Error('grant hl7-jira: ' + grantRes.status);
      }
    }

    document.getElementById('create-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      statusEl.textContent = 'Creating workspace…';
      const name = document.getElementById('workspace-name').value.trim();
      const topic = document.getElementById('topic-name').value.trim();
      const template = document.getElementById('template').value.trim();
      const localTools = Array.from(document.querySelectorAll('input[name="localTool"]:checked')).map(el => el.value);
      const jiraEnabled = document.getElementById('jira-enabled').checked;

      try {
        const createRes = await fetch(apiBase, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            name,
            template,
            topics: [{name: topic}],
            runtime: { localTools }
          })
        });
        if (!createRes.ok) {
          throw new Error('create workspace: ' + createRes.status);
        }
        if (jiraEnabled) {
          await registerDemoJiraTool(name);
        }
        statusEl.textContent = 'Workspace created.';
        window.location.href = '/app/' + encodeURIComponent(namespace) + '/' + encodeURIComponent(name) + '/' + encodeURIComponent(topic);
      } catch (err) {
        statusEl.textContent = err.message || String(err);
      }
    });

    document.getElementById('refresh-workspaces').addEventListener('click', async () => {
      statusEl.textContent = 'Refreshing…';
      try {
        await loadWorkspaces();
        statusEl.textContent = '';
      } catch (err) {
        statusEl.textContent = err.message || String(err);
      }
    });

    document.getElementById('save-participant').addEventListener('click', () => {
      const nextName = normalizeParticipantName(participantNameEl.value) || randomParticipantName();
      localStorage.setItem(participantKey, nextName);
      participantNameEl.value = nextName;
      participantStatusEl.textContent = 'Current participant: ' + nextName;
    });

    loadParticipantName();
    loadLocalTools().then(loadWorkspaces).catch(err => {
      statusEl.textContent = err.message || String(err);
    });
  </script>
</body>
</html>`))

var appTemplate = template.Must(template.New("app").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Workspace}} · {{.Topic}}</title>
  <style>
    :root { color-scheme: light; --bg:#f5f1e7; --card:#fffdf8; --line:#dbd2c6; --ink:#1f2430; --muted:#6b7280; --accent:#0f766e; }
    body { margin:0; font:16px/1.45 Georgia, serif; background:linear-gradient(180deg,#f8f3eb,#efe6d7); color:var(--ink);}
    main { max-width: 980px; margin: 0 auto; padding: 24px 20px 40px; }
    .card { background:var(--card); border:1px solid var(--line); border-radius:16px; padding:16px; box-shadow:0 12px 40px rgba(31,36,48,.08); margin-bottom:16px; }
    #messages { min-height: 300px; display:grid; gap:10px; }
    .msg { padding:10px 12px; border-radius:12px; background:#f7f2e8; border:1px solid #ece2d3; }
    .msg.system { background:#eef7f5; }
    .msg.error { background:#fff0f0; }
    .meta { color:var(--muted); font-size:13px; }
    textarea { width:100%; min-height:90px; box-sizing:border-box; padding:12px; border-radius:12px; border:1px solid var(--line); font:inherit; }
    button { border:0; border-radius:999px; background:var(--accent); color:#fff; padding:10px 16px; font:inherit; cursor:pointer; }
    button.secondary { background:#ece4d6; color:var(--ink); }
    button[disabled] { opacity:.55; cursor:default; }
    .row { display:flex; gap:10px; flex-wrap:wrap; align-items:center; }
    .action-link { display:inline-flex; align-items:center; justify-content:center; border-radius:999px; padding:10px 16px; text-decoration:none; }
    .action-link.primary { background:var(--accent); color:#fff; }
    .action-link.secondary { background:#ece4d6; color:var(--ink); }
    .composer-card { display:grid; gap:14px; }
    .queue-panel { border-top:1px solid #ece2d3; padding-top:14px; }
    .queue-list { display:grid; gap:10px; margin-top:12px; }
    .queue-entry { min-height:100px; padding:12px; border:1px solid #ece2d3; border-radius:12px; background:#f7f2e8; display:grid; gap:8px; }
    .queue-entry.own { border-color:#9ac7be; background:#eef7f5; }
    .queue-text { margin-top:6px; white-space:pre-wrap; }
    .queue-entry textarea { min-height:72px; margin:0; }
    .queue-header { justify-content:space-between; }
    .queue-actions { justify-content:flex-end; }
    input { box-sizing:border-box; padding:10px 12px; border-radius:12px; border:1px solid var(--line); font:inherit; background:#fff; }
    .inline-input { min-width:220px; max-width:320px; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size:13px; }
    pre { white-space: pre-wrap; background:#f7f2e8; border-radius:12px; padding:12px; overflow:auto; }
  </style>
</head>
<body data-ws-path="{{.WSPath}}" data-namespace="{{.Namespace}}" data-workspace="{{.Workspace}}" data-topic="{{.Topic}}">
  <main>
    <div class="card">
      <div class="row" style="justify-content:space-between;">
        <div>
          <h1 style="margin:0 0 8px;">{{.Workspace}}</h1>
          <div class="meta">Topic <code>{{.Topic}}</code> · Namespace <code>{{.Namespace}}</code></div>
        </div>
        <div class="row">
          <button id="delete-topic" type="button" class="secondary">Delete Topic</button>
          <a class="action-link secondary" href="/ws-language">WS Language Tutorial</a>
          <a class="action-link secondary" href="/shelley/{{.Namespace}}/{{.Workspace}}/{{.Topic}}">Open Shelley UI</a>
          <a class="action-link primary" href="/">Back</a>
        </div>
      </div>
      <pre>WS_MANAGER={{.Origin}} bun run cli.ts connect {{.Workspace}} {{.Topic}}</pre>
    </div>
    <div class="card">
      <div id="messages"></div>
    </div>
    <div class="card composer-card">
      <div class="queue-panel">
        <div class="row" style="justify-content:space-between;">
          <div>
            <h2 style="margin:0 0 8px;">Prompt Queue</h2>
            <div class="row" style="gap:8px; align-items:flex-end;">
              <div>
                <div class="meta">Participant Name</div>
                <input id="participant-name" class="inline-input" autocomplete="name" placeholder="Priya Shah">
              </div>
              <button id="save-participant" type="button" class="secondary">Use Name</button>
            </div>
            <div class="meta" style="margin-top:8px;">Currently connected as <code id="participant-id"></code></div>
          </div>
          <div class="row">
            <button id="refresh-queue" type="button" class="secondary">Refresh Queue</button>
            <button id="clear-my-queue" type="button" class="secondary" disabled>Clear My Queue</button>
          </div>
        </div>
        <div id="queue-summary" class="meta">Loading queue…</div>
        <div id="queue-list" class="queue-list"></div>
      </div>
      <form id="chat-form">
        <textarea id="prompt" placeholder="Ask Shelley to validate the IG, search HL7 Jira, or fix the profile."></textarea>
        <div class="row" style="margin-top:12px;">
          <button type="submit">Send Prompt</button>
          <span id="status" class="meta"></span>
        </div>
      </form>
    </div>
  </main>
  <script>
    const messagesEl = document.getElementById('messages');
    const statusEl = document.getElementById('status');
    const queueSummaryEl = document.getElementById('queue-summary');
    const queueListEl = document.getElementById('queue-list');
    const clearMyQueueButton = document.getElementById('clear-my-queue');
    const refreshQueueButton = document.getElementById('refresh-queue');
    const participantIdEl = document.getElementById('participant-id');
    const participantNameInput = document.getElementById('participant-name');
    const saveParticipantButton = document.getElementById('save-participant');
    const namespace = document.body.dataset.namespace;
    const workspace = document.body.dataset.workspace;
    const topic = document.body.dataset.topic;
    const queueApiBase = '/apis/v1/namespaces/' + encodeURIComponent(namespace) + '/workspaces/' + encodeURIComponent(workspace) + '/topics/' + encodeURIComponent(topic);
    const wsPath = document.body.dataset.wsPath;
    const wsScheme = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
    const participantKey = 'workspace-participant-id';
    const params = new URLSearchParams(window.location.search);
    function randomParticipantName() {
      return 'web-' + Math.random().toString(36).slice(2, 8);
    }
    function normalizeParticipantName(value) {
      return String(value == null ? '' : value).trim().replace(/\s+/g, ' ').slice(0, 64);
    }
    let participantId = normalizeParticipantName(params.get('client_id') || localStorage.getItem(participantKey) || '');
    if (!participantId) {
      participantId = randomParticipantName();
      localStorage.setItem(participantKey, participantId);
    }
    participantIdEl.textContent = participantId;
    participantNameInput.value = participantId;
    const wsURL = wsScheme + window.location.host + wsPath + '?client_id=' + encodeURIComponent(participantId);
    const conn = new WebSocket(wsURL);
    let wsOpened = false;
    let wsFailureShown = false;
    let promptCounter = 0;
    let queueRefreshTimer = null;
    let queueState = { activePromptId: '', entries: [] };

    function requestHeaders(extra) {
      return Object.assign({'X-Workspace-Client-ID': participantId}, extra || {});
    }

    async function readJSONResponse(res, label) {
      const text = await res.text();
      if (!res.ok) {
        throw new Error(label + ': ' + res.status + ' ' + (text.trim() || 'request failed'));
      }
      if (!text) return {};
      try {
        return JSON.parse(text);
      } catch (err) {
        throw new Error(label + ': invalid JSON response');
      }
    }

    function appendMessage(kind, title, body) {
      const div = document.createElement('div');
      div.className = 'msg ' + kind;
      const meta = document.createElement('div');
      meta.className = 'meta';
      meta.textContent = title;
      const content = document.createElement('div');
      content.textContent = body;
      div.appendChild(meta);
      div.appendChild(content);
      messagesEl.appendChild(div);
      div.scrollIntoView({behavior:'smooth', block:'end'});
    }

    function showConnectionFailure(message) {
      if (wsFailureShown) return;
      wsFailureShown = true;
      statusEl.textContent = 'Connection failed';
      appendMessage('error', 'Realtime connection failed', message);
    }

    function normalizeQueueSnapshot(snapshot) {
      return {
        activePromptId: snapshot && snapshot.activePromptId ? snapshot.activePromptId : '',
        entries: Array.isArray(snapshot && snapshot.entries) ? snapshot.entries : [],
      };
    }

    function isOwnQueueEntry(entry) {
      return !!(entry && entry.submittedBy && entry.submittedBy.id === participantId);
    }

    function applyQueueSnapshot(snapshot) {
      queueState = normalizeQueueSnapshot(snapshot);
      renderQueue();
    }

    function renderQueue() {
      queueListEl.replaceChildren();

      const ownEntries = queueState.entries.filter((entry) => isOwnQueueEntry(entry));
      clearMyQueueButton.disabled = ownEntries.length === 0;

      const queuedCount = queueState.entries.length;
      if (queueState.activePromptId) {
        queueSummaryEl.textContent = 'Active prompt ' + queueState.activePromptId + ' · ' + queuedCount + ' queued';
      } else if (queuedCount > 0) {
        queueSummaryEl.textContent = 'No active prompt · ' + queuedCount + ' queued';
      } else {
        queueSummaryEl.textContent = 'No queued prompts.';
      }

      if (queuedCount === 0) {
        const empty = document.createElement('div');
        empty.className = 'meta';
        empty.textContent = 'No queued prompts behind the current turn.';
        queueListEl.appendChild(empty);
        return;
      }

      for (const entry of queueState.entries) {
        const item = document.createElement('div');
        item.className = 'queue-entry';
        if (isOwnQueueEntry(entry)) {
          item.classList.add('own');
        }

        const header = document.createElement('div');
        header.className = 'row queue-header';

        const label = document.createElement('div');
        label.className = 'meta';
        const owner = entry.submittedBy && entry.submittedBy.id ? entry.submittedBy.id : 'unknown';
        const pieces = [];
        if (entry.position) pieces.push('#' + entry.position);
        pieces.push(entry.promptId || 'prompt');
        pieces.push(entry.status || 'queued');
        pieces.push('by ' + owner);
        label.textContent = pieces.join(' · ');
        header.appendChild(label);
        item.appendChild(header);

        const editor = document.createElement('textarea');
        editor.dataset.role = 'queue-text';
        editor.dataset.promptId = entry.promptId || '';
        editor.defaultValue = entry.text || '';
        editor.value = entry.text || '';
        item.appendChild(editor);

        const actions = document.createElement('div');
        actions.className = 'row queue-actions';

        const saveButton = document.createElement('button');
        saveButton.type = 'button';
        saveButton.className = 'secondary';
        saveButton.dataset.promptId = entry.promptId || '';
        saveButton.dataset.action = 'save-queued';
        saveButton.textContent = 'Save';
        actions.appendChild(saveButton);

        const topButton = document.createElement('button');
        topButton.type = 'button';
        topButton.className = 'secondary';
        topButton.dataset.promptId = entry.promptId || '';
        topButton.dataset.action = 'move-top';
        topButton.textContent = 'Top';
        topButton.disabled = !entry.position || entry.position <= 1;
        actions.appendChild(topButton);

        const upButton = document.createElement('button');
        upButton.type = 'button';
        upButton.className = 'secondary';
        upButton.dataset.promptId = entry.promptId || '';
        upButton.dataset.action = 'move-up';
        upButton.textContent = 'Up';
        upButton.disabled = !entry.position || entry.position <= 1;
        actions.appendChild(upButton);

        const downButton = document.createElement('button');
        downButton.type = 'button';
        downButton.className = 'secondary';
        downButton.dataset.promptId = entry.promptId || '';
        downButton.dataset.action = 'move-down';
        downButton.textContent = 'Down';
        downButton.disabled = !entry.position || entry.position >= queuedCount;
        actions.appendChild(downButton);

        const bottomButton = document.createElement('button');
        bottomButton.type = 'button';
        bottomButton.className = 'secondary';
        bottomButton.dataset.promptId = entry.promptId || '';
        bottomButton.dataset.action = 'move-bottom';
        bottomButton.textContent = 'Bottom';
        bottomButton.disabled = !entry.position || entry.position >= queuedCount;
        actions.appendChild(bottomButton);

        const deleteButton = document.createElement('button');
        deleteButton.type = 'button';
        deleteButton.className = 'secondary';
        deleteButton.dataset.promptId = entry.promptId || '';
        deleteButton.dataset.action = 'cancel-queued';
        deleteButton.textContent = 'Delete';
        actions.appendChild(deleteButton);

        item.appendChild(actions);

        queueListEl.appendChild(item);
      }
    }

    async function refreshQueue() {
      const res = await fetch(queueApiBase + '/queue', {
        headers: requestHeaders(),
      });
      applyQueueSnapshot(await readJSONResponse(res, 'load queue'));
    }

    function scheduleQueueRefresh() {
      if (queueRefreshTimer !== null) return;
      queueRefreshTimer = window.setTimeout(async () => {
        queueRefreshTimer = null;
        try {
          await refreshQueue();
        } catch (err) {
          queueSummaryEl.textContent = err.message || String(err);
        }
      }, 75);
    }

    async function cancelQueuedPrompt(promptId) {
      if (!promptId) return;
      const res = await fetch(queueApiBase + '/queue/' + encodeURIComponent(promptId), {
        method: 'DELETE',
        headers: requestHeaders(),
      });
      if (!res.ok && res.status !== 204) {
        throw new Error('cancel queued prompt: ' + res.status + ' ' + ((await res.text()).trim() || 'request failed'));
      }
      scheduleQueueRefresh();
    }

    async function updateQueuedPrompt(promptId, text) {
      if (!promptId) return;
      const nextText = (text || '').trim();
      if (!nextText) {
        throw new Error('queued prompt text is required');
      }
      const res = await fetch(queueApiBase + '/queue/' + encodeURIComponent(promptId), {
        method: 'PATCH',
        headers: requestHeaders({'Content-Type': 'application/json'}),
        body: JSON.stringify({text: nextText}),
      });
      applyQueueSnapshot(await readJSONResponse(res, 'update queued prompt'));
    }

    async function moveQueuedPrompt(promptId, direction) {
      if (!promptId) return;
      const res = await fetch(queueApiBase + '/queue/' + encodeURIComponent(promptId) + '/move', {
        method: 'POST',
        headers: requestHeaders({'Content-Type': 'application/json'}),
        body: JSON.stringify({direction}),
      });
      applyQueueSnapshot(await readJSONResponse(res, 'move queued prompt'));
    }

    async function clearMyQueue() {
      const res = await fetch(queueApiBase + '/queue:clear-mine', {
        method: 'POST',
        headers: requestHeaders(),
      });
      const body = await readJSONResponse(res, 'clear my queue');
      const removed = Array.isArray(body.removed) ? body.removed : [];
      appendMessage('system', 'Queue', removed.length > 0 ? ('cleared ' + removed.join(', ')) : 'no queued prompts to clear');
      scheduleQueueRefresh();
    }

    statusEl.textContent = 'Connecting...';
    conn.onopen = () => {
      wsOpened = true;
      statusEl.textContent = 'Connected';
      scheduleQueueRefresh();
    };
    conn.onclose = () => {
      if (!wsOpened) {
        showConnectionFailure('The topic websocket could not connect. Reload after the workspace is ready or check the manager logs.');
        return;
      }
      statusEl.textContent = 'Disconnected';
    };
    conn.onerror = () => {
      if (!wsOpened) {
        showConnectionFailure('The topic websocket was rejected before the session could start.');
        return;
      }
      statusEl.textContent = 'WebSocket error';
    };
    conn.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      switch (msg.type) {
        case 'connected':
          appendMessage('system', 'Connected', 'Session ' + (msg.sessionId || ''));
          break;
        case 'prompt_status':
          appendMessage('system', 'Prompt Status', (msg.promptId || 'prompt') + ' ' + (msg.status || '') + (msg.position ? ' (#' + msg.position + ')' : ''));
          scheduleQueueRefresh();
          break;
        case 'queue_snapshot':
          applyQueueSnapshot(msg);
          break;
        case 'queue_entry_updated':
        case 'queue_entry_moved':
          scheduleQueueRefresh();
          break;
        case 'queue_entry_removed':
          scheduleQueueRefresh();
          break;
        case 'queue_cleared':
          scheduleQueueRefresh();
          break;
        case 'user':
          appendMessage('system', (msg.submittedBy && msg.submittedBy.id ? msg.submittedBy.id : 'User'), msg.data || '');
          break;
        case 'text':
          appendMessage('', 'Assistant', msg.data || '');
          break;
        case 'tool_call':
          appendMessage('system', 'Tool Call', (msg.title || msg.tool || '') + ' · ' + (msg.status || 'started'));
          break;
        case 'tool_update':
          appendMessage('system', 'Tool Update', (msg.title || msg.tool || '') + ' · ' + (msg.status || ''));
          break;
        case 'system':
          appendMessage('system', 'System', msg.data || '');
          break;
        case 'error':
          appendMessage('error', 'Error', msg.data || 'Unknown error');
          break;
        case 'done':
          appendMessage('system', 'Done', 'Turn finished');
          scheduleQueueRefresh();
          break;
        default:
          appendMessage('system', msg.type, event.data);
      }
    };

    document.getElementById('chat-form').addEventListener('submit', (event) => {
      event.preventDefault();
      const input = document.getElementById('prompt');
      const text = input.value.trim();
      if (!text) return;
      if (conn.readyState !== WebSocket.OPEN) {
        showConnectionFailure('Cannot send a prompt because the realtime connection is not open.');
        return;
      }
      promptCounter += 1;
      conn.send(JSON.stringify({type: 'prompt', promptId: 'p_web_' + Date.now() + '_' + promptCounter, data: text}));
      input.value = '';
    });

    saveParticipantButton.addEventListener('click', () => {
      const nextName = normalizeParticipantName(participantNameInput.value) || randomParticipantName();
      localStorage.setItem(participantKey, nextName);
      const nextURL = new URL(window.location.href);
      nextURL.searchParams.set('client_id', nextName);
      window.location.href = nextURL.toString();
    });

    refreshQueueButton.addEventListener('click', async () => {
      try {
        await refreshQueue();
      } catch (err) {
        appendMessage('error', 'Queue', err.message || String(err));
      }
    });

    clearMyQueueButton.addEventListener('click', async () => {
      try {
        await clearMyQueue();
      } catch (err) {
        appendMessage('error', 'Queue', err.message || String(err));
      }
    });

    queueListEl.addEventListener('click', async (event) => {
      const button = event.target.closest('button[data-action]');
      if (!button) return;
      try {
        const promptId = button.dataset.promptId || '';
        const action = button.dataset.action || '';
        if (action === 'cancel-queued') {
          await cancelQueuedPrompt(promptId);
          return;
        }
        if (action === 'move-up') {
          await moveQueuedPrompt(promptId, 'up');
          return;
        }
        if (action === 'move-top') {
          await moveQueuedPrompt(promptId, 'top');
          return;
        }
        if (action === 'move-down') {
          await moveQueuedPrompt(promptId, 'down');
          return;
        }
        if (action === 'move-bottom') {
          await moveQueuedPrompt(promptId, 'bottom');
          return;
        }
        if (action === 'save-queued') {
          const item = button.closest('.queue-entry');
          const editor = item ? item.querySelector('textarea[data-role="queue-text"]') : null;
          await updateQueuedPrompt(promptId, editor ? editor.value : '');
        }
      } catch (err) {
        appendMessage('error', 'Queue', err.message || String(err));
      }
    });

    document.getElementById('delete-topic').addEventListener('click', async () => {
      if (!window.confirm('Delete topic "' + topic + '"?')) return;
      const res = await fetch('/apis/v1/namespaces/' + encodeURIComponent(namespace) + '/workspaces/' + encodeURIComponent(workspace) + '/topics/' + encodeURIComponent(topic), {
        method: 'DELETE'
      });
      if (!res.ok) {
        appendMessage('error', 'Delete Topic Failed', 'HTTP ' + res.status);
        return;
      }
      window.location.href = '/';
    });
  </script>
</body>
</html>`))

var wsLanguageTemplate = template.Must(template.New("ws-language").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>WS Language Tutorial</title>
  <style>
    :root { color-scheme: light; --bg:#f4f0e8; --ink:#1f2430; --muted:#6b7280; --card:#fffdf8; --line:#d7cec0; --accent:#0f766e; --accent-ink:#ffffff; }
    body { margin:0; font:16px/1.5 Georgia, serif; background: radial-gradient(circle at top left, #f8f4eb, #efe6d8 60%, #e7ddcf); color:var(--ink); }
    main { max-width: 980px; margin: 0 auto; padding: 28px 20px 56px; display:grid; gap:18px; }
    .card { background: var(--card); border: 1px solid var(--line); border-radius: 16px; padding: 18px; box-shadow: 0 12px 40px rgba(31,36,48,.08); }
    h1,h2,h3 { margin:0 0 10px; font-family: "Iowan Old Style", Georgia, serif; }
    .grid { display:grid; gap:16px; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); }
    .muted { color: var(--muted); }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size:13px; }
    pre { white-space: pre-wrap; background: #f7f2e8; border-radius: 12px; padding: 12px; overflow:auto; }
    ul { margin: 0; padding-left: 18px; }
    .action-link { display:inline-flex; align-items:center; justify-content:center; border-radius:999px; padding:10px 16px; text-decoration:none; background:#ece4d6; color:var(--ink); }
  </style>
</head>
<body>
  <main>
    <div class="card">
      <div style="display:flex; justify-content:space-between; gap:12px; align-items:flex-start; flex-wrap:wrap;">
        <div>
          <h1>WS Language Tutorial</h1>
          <p class="muted">Use <code>ws ...</code> prompts with the predictable model to script live demo behavior on the fly. Tags can appear in any order; only one primary action is allowed per prompt.</p>
        </div>
        <a class="action-link" href="/">Back To Shelley Manager</a>
      </div>
    </div>

    <div class="grid">
      <section class="card">
        <h2>Primary Actions</h2>
        <ul>
          <li><code>text</code> or <code>echo</code>: return assistant text immediately.</li>
          <li><code>bash</code>: call Shelley’s built-in <code>bash</code> tool with your command.</li>
          <li><code>validator</code>: run the local <code>fhir-validator</code> wrapper through <code>bash</code>.</li>
          <li><code>publisher</code>: run the local <code>ig-publisher</code> wrapper through <code>bash</code>.</li>
          <li><code>jira</code>: call the first-class <code>hl7-jira</code> MCP tool as <code>jira.search</code>.</li>
          <li><code>tool</code> + <code>action</code> + optional <code>input</code>: call any registered workspace tool explicitly.</li>
        </ul>
      </section>

      <section class="card">
        <h2>Timing Tags</h2>
        <ul>
          <li><code>pause2</code> or <code>pause 2</code>: delay before the assistant responds or starts a tool.</li>
          <li><code>toolpause3</code> or <code>toolpause 3</code>: keep the tool busy for 3 seconds. This is the easiest way to demonstrate queueing.</li>
          <li><code>afterpause1</code>: delay the follow-up assistant text after a tool result arrives.</li>
          <li><code>aftertext "..."</code>: customize what the predictable model says after the tool call finishes.</li>
        </ul>
      </section>
    </div>

    <section class="card">
      <h2>Demo-Ready Examples</h2>
      <pre>ws text "Thanks. Let me summarize the validator findings."</pre>
      <pre>ws pause2 validator "input/fsh/BloodPressurePanel.fsh" toolpause3 aftertext "The validator is pointing at missing slicing metadata on Observation.component."</pre>
      <pre>ws jira "Observation.component slicing validator failure" pause1</pre>
      <pre>ws tool hl7-jira action jira.search input '{"query":"validator warning blood pressure slicing"}' aftertext "I found two relevant HL7 Jira threads."</pre>
      <pre>ws bash "sed -n '1,160p' input/fsh/BloodPressurePanel.fsh"</pre>
    </section>

    <section class="card">
      <h2>Queueing Trick</h2>
      <p>To show queueing live in the demo, make the current turn visibly slow at the exact point you want:</p>
      <pre>ws validator "input/fsh/BloodPressurePanel.fsh" toolpause5 aftertext "Validator run finished."</pre>
      <p class="muted">While that five-second validator step is running, submit another prompt from the browser or CLI. The second prompt will queue, and the queue panel will let you edit, reorder, move to top/bottom, or delete it before it runs.</p>
    </section>

    <section class="card">
      <h2>Rules Of Thumb</h2>
      <ul>
        <li>Wrap multi-word values in single or double quotes.</li>
        <li><code>input</code> must be valid JSON.</li>
        <li>Use only one primary action in a single prompt.</li>
        <li><code>ws help</code> in a predictable-model chat returns a compact version of this tutorial.</li>
      </ul>
    </section>
  </main>
</body>
</html>`))

type homeTemplateData struct {
	Namespace string
}

type appTemplateData struct {
	Namespace string
	Workspace string
	Topic     string
	Origin    string
	WSPath    string
}

func (m *Manager) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = homeTemplate.Execute(w, homeTemplateData{
		Namespace: m.defaultNamespace,
	})
}

func (m *Manager) handleWSLanguage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = wsLanguageTemplate.Execute(w, nil)
}

func (m *Manager) handleDemoJiraScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(demoHL7JiraMCPFixtureScript))
}

func (m *Manager) handleApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/app/"))
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	namespace, workspace, topic := parts[0], parts[1], parts[2]
	if _, ok := m.getWorkspace(namespace, workspace); !ok {
		http.NotFound(w, r)
		return
	}
	data := appTemplateData{
		Namespace: namespace,
		Workspace: workspace,
		Topic:     topic,
		Origin:    requestBase(r, false),
		WSPath:    "/acp/" + namespace + "/" + workspace + "/topics/" + topic,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = appTemplate.Execute(w, data)
}

func (m *Manager) handleShelleyUIRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := splitPath(strings.TrimPrefix(r.URL.Path, "/shelley/"))
	if len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	namespace, workspace := parts[0], parts[1]
	ws, ok := m.getWorkspace(namespace, workspace)
	if !ok {
		http.NotFound(w, r)
		return
	}
	target := strings.TrimRight(ws.Runtime.APIBase.String(), "/")
	if len(parts) == 3 {
		target += "/c/" + parts[2]
	}
	http.Redirect(w, r, target, http.StatusFound)
}
