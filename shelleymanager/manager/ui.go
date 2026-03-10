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
    .workspace { padding:12px; border-top:1px solid var(--line); }
    .workspace:first-child { border-top:0; padding-top:0; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
    pre { white-space: pre-wrap; background: #f7f2e8; border-radius: 12px; padding: 12px; overflow:auto; }
    .status { min-height: 22px; color: var(--muted); }
    @media (max-width: 860px) { .grid { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main>
    <div class="card" style="margin-bottom:20px;">
      <h1>Shelley Manager Demo</h1>
      <p class="muted">Create a workspace from the manager-published local tool catalog, then optionally pre-register the HL7 Jira MCP tool with the real workspace APIs.</p>
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
        <div id="workspaces" class="muted">Loading…</div>
      </section>
    </div>
  </main>
  <script>
    const namespace = {{printf "%q" .Namespace}};
    const jiraFixtureScript = {{printf "%q" .JiraScript}};
    const apiBase = '/apis/v1/namespaces/' + encodeURIComponent(namespace) + '/workspaces';
    const localToolsEl = document.getElementById('local-tools');
    const workspacesEl = document.getElementById('workspaces');
    const statusEl = document.getElementById('status');

    async function loadLocalTools() {
      const res = await fetch('/apis/v1/local-tools');
      const tools = await res.json();
      if (!Array.isArray(tools) || tools.length === 0) {
        localToolsEl.innerHTML = '<p class="muted">No local tools published by this manager.</p>';
        return;
      }
      localToolsEl.innerHTML = tools.map((tool, idx) => {
        const requires = (tool.requirements && tool.requirements.length)
          ? '<div class="muted">Requires: ' + tool.requirements.join(', ') + '</div>' : '';
        const commands = (tool.commands && tool.commands.length)
          ? '<div class="muted">Commands: ' + tool.commands.map(c => '<code>' + c.name + '</code>').join(', ') + '</div>' : '';
        const checked = idx === 0 ? 'checked' : '';
        return '<label class="tool">'
          + '<div class="row">'
          + '<input type="checkbox" name="localTool" value="' + tool.name + '" ' + checked + ' style="width:auto;">'
          + '<strong>' + tool.name + '</strong>'
          + '</div>'
          + '<div class="muted">' + (tool.description || '') + '</div>'
          + requires
          + commands
          + '</label>';
      }).join('');
    }

    function workspaceCard(ws) {
      const topic = (ws.topics && ws.topics[0] && ws.topics[0].name) || 'bp-panel-validator';
      const openHref = '/app/' + encodeURIComponent(ws.namespace || namespace) + '/' + encodeURIComponent(ws.name) + '/' + encodeURIComponent(topic);
      const cli = 'WS_MANAGER=' + window.location.origin + ' bun run cli.ts connect ' + ws.name + ' ' + topic;
      const localTools = ws.runtime && ws.runtime.localTools ? ws.runtime.localTools.map(t => '<code>' + t.name + '</code>').join(', ') : '<span class="muted">none</span>';
      return '<div class="workspace">'
        + '<div class="row" style="justify-content:space-between;">'
        + '<strong>' + ws.name + '</strong>'
        + '<span class="muted">' + ws.status + '</span>'
        + '</div>'
        + '<div class="muted">Topic: <code>' + topic + '</code></div>'
        + '<div class="muted">Local tools: ' + localTools + '</div>'
        + '<div class="row" style="margin-top:10px;">'
        + '<a href="' + openHref + '"><button type="button">Open Topic</button></a>'
        + '</div>'
        + '<pre>' + cli + '</pre>'
        + '</div>';
    }

    async function loadWorkspaces() {
      const res = await fetch(apiBase);
      const workspaces = await res.json();
      if (!Array.isArray(workspaces) || workspaces.length === 0) {
        workspacesEl.innerHTML = '<p class="muted">No workspaces yet.</p>';
        return;
      }
      const details = await Promise.all(workspaces.map(async ws => {
        const detailRes = await fetch(apiBase + '/' + encodeURIComponent(ws.name));
        return detailRes.json();
      }));
      workspacesEl.innerHTML = details.map(workspaceCard).join('');
    }

    async function registerDemoJiraTool(name) {
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
        await loadWorkspaces();
        window.location.href = '/app/' + encodeURIComponent(namespace) + '/' + encodeURIComponent(name) + '/' + encodeURIComponent(topic);
      } catch (err) {
        statusEl.textContent = err.message || String(err);
      }
    });

    document.getElementById('refresh-workspaces').addEventListener('click', async () => {
      statusEl.textContent = 'Refreshing…';
      await loadWorkspaces();
      statusEl.textContent = '';
    });

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
    .row { display:flex; gap:10px; flex-wrap:wrap; align-items:center; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size:13px; }
    pre { white-space: pre-wrap; background:#f7f2e8; border-radius:12px; padding:12px; overflow:auto; }
  </style>
</head>
<body>
  <main>
    <div class="card">
      <div class="row" style="justify-content:space-between;">
        <div>
          <h1 style="margin:0 0 8px;">{{.Workspace}}</h1>
          <div class="meta">Topic <code>{{.Topic}}</code> · Namespace <code>{{.Namespace}}</code></div>
        </div>
        <a href="/"><button type="button">Back</button></a>
      </div>
      <pre>WS_MANAGER={{.Origin}} bun run cli.ts connect {{.Workspace}} {{.Topic}}</pre>
    </div>
    <div class="card">
      <div id="messages"></div>
    </div>
    <div class="card">
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
    const wsURL = {{printf "%q" .WSURL}};
    const conn = new WebSocket(wsURL);

    function appendMessage(kind, title, body) {
      const div = document.createElement('div');
      div.className = 'msg ' + kind;
      div.innerHTML = '<div class="meta">' + title + '</div><div>' + body + '</div>';
      messagesEl.appendChild(div);
      div.scrollIntoView({behavior:'smooth', block:'end'});
    }

    conn.onopen = () => { statusEl.textContent = 'Connected'; };
    conn.onclose = () => { statusEl.textContent = 'Disconnected'; };
    conn.onerror = () => { statusEl.textContent = 'WebSocket error'; };
    conn.onmessage = (event) => {
      const msg = JSON.parse(event.data);
      switch (msg.type) {
        case 'connected':
          appendMessage('system', 'Connected', 'Session <code>' + msg.sessionId + '</code>');
          break;
        case 'prompt_status':
          appendMessage('system', 'Prompt Status', '' + msg.status);
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
      conn.send(JSON.stringify({type: 'prompt', data: text}));
      appendMessage('system', 'You', text);
      input.value = '';
    });
  </script>
</body>
</html>`))

type homeTemplateData struct {
	Namespace  string
	JiraScript string
}

type appTemplateData struct {
	Namespace string
	Workspace string
	Topic     string
	Origin    string
	WSURL     string
}

func (m *Manager) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = homeTemplate.Execute(w, homeTemplateData{
		Namespace:  m.defaultNamespace,
		JiraScript: demoHL7JiraMCPFixtureScript,
	})
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
		WSURL:     requestBase(r, true) + "/acp/" + namespace + "/" + workspace + "/topics/" + topic,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = appTemplate.Execute(w, data)
}
