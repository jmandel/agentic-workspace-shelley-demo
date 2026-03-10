# Demo Run Of Show: Fixing Broken Blood Pressure Example Resources

## Demo Goal

Show one believable collaborative standards workflow:

- Priya creates a fresh isolated workspace through Shelley Manager.
- She opens a shared topic in the browser and asks Shelley to validate two
  broken example resources from the IG.
- Shelley uses a trusted local runtime tool to run the real FHIR Validator JAR
  inside the workspace.
- Marco joins late from the CLI, catches up to the in-progress topic, and asks
  Shelley to search related HL7 Jira issues through an MCP stdio tool.
- Shelley fixes the example JSON files and re-runs validation.

This should feel like a real standards-team debugging session, not a synthetic
"agent does generic task" demo.

## Cast

- Priya Shah
  - implementation guide editor
  - working in the browser
- Marco Ruiz
  - validator/debug specialist
  - joining late from the CLI
- Shelley Manager
  - public entrypoint
  - creates isolated workspaces
  - proxies browser and CLI traffic into each workspace runtime
- Shelley Runtime
  - one runtime per workspace
  - launched in `bwrap` mode for the demo

## Story World

The team is working on the fictional but plausible `acme-rpm-ig` repository:

- name: `Acme Remote Patient Monitoring Implementation Guide`
- focus area for this demo: home blood pressure readings
- current release pressure: they want a preview build ready for tomorrow's work
  group review

The concrete bug is in two example resources that are supposed to appear in the
guide:

- `input/examples/Patient-bp-alice-smith.json`
- `input/examples/Observation-bp-alice-morning.json`

These files are realistic things the validator can actually help with:

- the Patient example uses an invalid administrative gender code
- the Patient example has an invalid birth date
- the Observation example has an invalid `effectiveDateTime`
- the Observation example is missing the systolic and diastolic components
  required for a blood pressure panel

## Fixed Demo Data

Use these exact demo values every time.

- workspace name: `bp-ig-fix`
- namespace: `acme`
- repo/template label in the create form: `acme-rpm-ig`
- topic name: `bp-example-validator`

## On-Stage Safety Net

If the presenter needs a live reminder of the predictable demo syntax, type one
of these directly into the topic:

- `ws`
- `ws help`
- `ws usage`

Each returns the full in-chat WS language guide, including the exact validator,
Jira, and fix-up commands used in this demo.

## Tool Model In This Demo

The mainline demo should show only two tool paths.

### 1. Trusted local runtime tools

These are mounted into the isolated workspace and are reachable through bash.
They are not registered through the workspace tool API and they are not shown
to Shelley as first-class `workspace_*` tools.

Main demo example:

- `fhir-validator`

Possible follow-on example if there is extra time:

- `ig-publisher`

### 2. Managed MCP workspace tools

These are registered through the workspace tool API and surfaced to Shelley as
first-class `workspace_*` tools.

Main demo example:

- `hl7-jira`

Important narration point:

- Shelley sees `hl7-jira` as a first-class workspace tool
- the real MCP binding is held by the Shelley Manager, not the workspace runtime
- in this demo, the manager runs a Bun MCP server script on the host side and
  brokers calls to it over an internal HTTP bridge
- this keeps the MCP transport details out of the workspace while still giving
  Shelley the tool shape it needs

Example configuration shape:

```json
{
  "protocol": "mcp",
  "transport": {
    "type": "manager_proxy"
  }
}
```

### What We Should Not Mix Into The Mainline Demo

We should not try to demonstrate every possible tool category in one story.

In particular, approval-gated tools should be kept out of the mainline demo
unless approval itself is the headline feature we want to emphasize.

## API Calls In This Demo

The demo should make one thing very clear:

- `fhir-validator` is not created through the workspace tools API
- `hl7-jira` is created through the normal workspace tools API
- the manager brokers the real MCP transport behind that API without exposing
  its executable/db details to Shelley

The two setup paths are intentionally different.

### 0. Inspect the manager's local tool catalog

Before creating the workspace, the demo can show that the manager publishes the
local runtime tools it knows how to provide.

```http
GET /apis/v1/local-tools
```

Expected result for the demo:

- `fhir-validator`
- `hl7-jira-support`
- optionally `ig-publisher`

The point is that the string name is not opaque manager magic. The manager
publishes what those names mean.

### 1. Create the workspace and select local tools from the catalog

This is a Shelley Manager call.

It creates the isolated workspace, pre-creates the topic, and tells the manager
which published local tools to enable in the runtime.

```http
POST /apis/v1/namespaces/acme/workspaces
Content-Type: application/json

{
  "name": "bp-ig-fix",
  "template": "acme-rpm-ig",
  "topics": [
    { "name": "bp-example-validator" }
  ],
  "runtime": {
    "localTools": ["fhir-validator", "hl7-jira-support"]
  }
}
```

Meaning:

- `runtime.localTools` is manager-controlled runtime setup, not a workspace tool
  registration surface
- for the main demo, this is how `fhir-validator` becomes available inside the
  bubblewrapped workspace
- `hl7-jira-support` is a manager-published support bundle, not a first-class
  bash tool; it mounts the Bun MCP entrypoint and the real Jira SQLite snapshot
  at stable `/tools/hl7-jira-support/...` paths
- the manager should also write workspace guidance so Shelley knows that
  `fhir-validator` is available through bash at a known path such as
  `/tools/bin/fhir-validator`

### 2. Register the Jira MCP tool through the workspace tools API

This is the public manager-proxied workspace tools API call.

```http
POST /apis/v1/namespaces/acme/workspaces/bp-ig-fix/tools
Content-Type: application/json

{
  "name": "hl7-jira",
  "description": "Search the real HL7 Jira snapshot",
  "provider": "demo@acme.example",
  "protocol": "mcp",
  "transport": {
    "type": "stdio",
    "command": "bun",
    "args": ["/tools/hl7-jira-support/bin/hl7-jira-mcp.js"],
    "cwd": "/tools/hl7-jira-support",
    "env": {
      "HL7_JIRA_DB": "/tools/hl7-jira-support/data/jira-data.db"
    }
  },
  "tools": [
    {
      "name": "jira.search",
      "title": "Search HL7 Jira",
      "description": "Search HL7 Jira issues related to validator behavior",
      "inputSchema": {
        "type": "object",
        "properties": {
          "query": { "type": "string" },
          "limit": { "type": "integer", "minimum": 1, "maximum": 10 }
        },
        "required": ["query"],
        "additionalProperties": false
      }
    }
  ]
}
```

Meaning:

- the client tells the server a normal MCP binding
- the manager stores the real transport binding outside the workspace runtime
- the manager registers a sanitized internal `manager_proxy` transport inside
  Shelley
- Shelley learns the tool shape, but not the real launch details

### 3. Grant the agent access to the MCP tool

This is also an internal workspace runtime API call, issued by the manager for
the demo setup.

```http
POST /apis/v1/namespaces/acme/workspaces/bp-ig-fix/tools/hl7-jira/grants
Content-Type: application/json

{
  "subject": "agent:*",
  "tools": ["jira.search"],
  "access": "allowed",
  "approvers": [],
  "scope": {}
}
```

Meaning:

- the agent can use `jira.search` without per-call approval
- this is the only tool grant needed for the mainline demo

## Tools In The Demo

### `fhir-validator`

- kind: trusted local runtime tool
- access pattern: Shelley reaches it through bash inside the workspace runtime
- implementation: wrapper script around the real FHIR Validator JAR
- purpose: validate example JSON resources directly and surface real validator
  diagnostics

### `hl7-jira`

- kind: MCP stdio workspace tool
- implementation: SDK-backed Bun MCP server launched by Shelley Manager against
  the mounted `hl7-jira-support` bundle
- backing data: the real `fhir-community-search` Jira SQLite snapshot mounted at
  `/tools/hl7-jira-support/data/jira-data.db`
- purpose: search real HL7 Jira issues derived from `fhir-community-search`
- tool name exposed through MCP: `jira.search`

Typical results for the demo query `validation error handling` include:

- `FHIR-20482`
  - title: `FHIRPath conformsTo Validation of Warnings/Error handling pull request`
- `FHIR-31991`
  - title: `Specify behavior for incorrect type returned by expressions`

## Starting Files

At demo start, the example resources should look roughly like this.

### `input/examples/Patient-bp-alice-smith.json`

```json
{
  "resourceType": "Patient",
  "id": "alice-smith",
  "active": true,
  "name": [
    {
      "family": "Smith",
      "given": ["Alice"]
    }
  ],
  "gender": "woman",
  "birthDate": "1974-25-12"
}
```

### `input/examples/Observation-bp-alice-morning.json`

```json
{
  "resourceType": "Observation",
  "id": "bp-alice-morning",
  "status": "final",
  "category": [
    {
      "coding": [
        {
          "system": "http://terminology.hl7.org/CodeSystem/observation-category",
          "code": "vital-signs"
        }
      ]
    }
  ],
  "code": {
    "coding": [
      {
        "system": "http://loinc.org",
        "code": "85354-9",
        "display": "Blood pressure panel with all children optional"
      }
    ]
  },
  "subject": {
    "reference": "Patient/alice-smith"
  },
  "effectiveDateTime": "2026-02-30T07:00:00Z"
}
```

These are exactly the sort of problems the real validator can help with.

## Representative Validator Output

The validator output should be recognizably real and close to what the official
CLI emits.

```text
*FAILURE*: 3 errors, 1 warnings, 0 notes
Error @ Patient.gender: The value provided ('woman') was not found in the value set 'AdministrativeGender'
Error @ Patient.birthDate: Not a valid date format: '1974-25-12'

*FAILURE*: 5 errors, 2 warnings, 2 notes
Error @ Observation.effective.ofType(dateTime): Not a valid date format: '2026-02-30T07:00:00Z'
Error @ Observation: Observation.component: minimum required = 2, but only found 0
Error @ Observation: Slice 'Observation.component:SystolicBP' was not found
Error @ Observation: Slice 'Observation.component:DiastolicBP' was not found
Error @ Observation: Constraint failed: vs-2
```

Shelley should summarize this in plain language:

> The validator is finding real example-data problems, not profile-compiler
> issues. The Patient example uses a non-FHIR gender code and an invalid birth
> date, and the Observation example has an impossible `effectiveDateTime` plus
> missing blood-pressure components.

## Run Of Show

### 1. Priya creates the workspace

Priya opens Shelley Manager and fills in:

- Namespace: `acme`
- Workspace name: `bp-ig-fix`
- Template: `acme-rpm-ig`

She clicks `Create Workspace`.

The page should show:

- status: `running`
- share card with:
  - browser URL:
    `https://demo.example.org/apis/v1/namespaces/acme/workspaces/bp-ig-fix`
  - CLI join command:

```bash
WS_MANAGER=https://demo.example.org bun run cli.ts connect bp-ig-fix bp-example-validator
```

Presenter line:

> Priya is creating a fresh isolated workspace for one focused standards task.
> Shelley Manager is the only public endpoint. The Shelley runtime itself is
> private and running under bubblewrap.

### 2. Priya opens the shared topic in the browser

Priya opens topic `bp-example-validator`.

Her first prompt is exactly:

> Run the FHIR validator on `input/examples/Patient-bp-alice-smith.json` and `input/examples/Observation-bp-alice-morning.json`, then explain what is broken.

The audience should understand:

- this is Shelley inside the workspace
- the browser is still talking through Shelley Manager
- topic state lives inside this one workspace runtime

### 3. Shelley runs the validator and finds the actual bugs

Shelley invokes `fhir-validator`.

What the audience sees:

- a real tool call for `fhir-validator`
- concrete validator errors about:
  - invalid administrative gender code
  - invalid birth date
  - invalid observation effective date/time
  - missing systolic and diastolic blood pressure components
- Shelley's explanation that the examples need data cleanup before publication

Presenter line:

> This first tool is deliberately not MCP. It is a trusted local runtime tool
> mounted into the isolated workspace and reachable through bash. We want to
> show that the workspace can host both local runtime tools and MCP-backed
> tools.

### 4. Marco joins late from the CLI and catches up

Marco arrives after the validator has already run.

He uses the share command:

```bash
WS_MANAGER=https://demo.example.org bun run cli.ts connect bp-ig-fix bp-example-validator
```

What Marco should see immediately:

- the current topic connection
- replay of the recent topic events from the active session
- the validator tool call
- the validator errors
- Shelley's current summary of the problem

Presenter line:

> Marco is joining late, but he is not joining blind. The topic websocket
> catches him up to the live session, so browser and CLI participants can share
> one in-progress debugging conversation.

### 5. Marco asks Shelley to search HL7 Jira

Marco types this exact prompt in the CLI:

> Search HL7 Jira for issues about validator error handling for bad codes and invalid dates in example resources.

Shelley invokes the manager-brokered MCP tool `hl7-jira`, specifically the MCP
tool `jira.search`.

The real Jira snapshot returns results like:

- `FHIR-20482` — `FHIRPath conformsTo Validation of Warnings/Error handling pull request`
- `FHIR-31991` — `Specify behavior for incorrect type returned by expressions`

Shelley summarizes:

> The Jira results reinforce that validator error handling is still an active
> topic, but the fixes for these examples are straightforward: use a valid
> administrative gender code and correct the invalid date and dateTime fields.

### 6. Priya asks Shelley to fix the examples and re-run validation

Priya types this exact prompt in the browser:

> Update those two example JSON files to use valid FHIR values, then run validation again.

Shelley edits:

- `Patient-bp-alice-smith.json`
  - `gender: "female"`
  - `birthDate: "1974-12-25"`
- `Observation-bp-alice-morning.json`
  - `effectiveDateTime: "2026-02-28T07:00:00Z"`
  - adds `component` entries for:
    - systolic blood pressure (`8480-6`)
    - diastolic blood pressure (`8462-4`)

Shelley then re-runs `fhir-validator`.

Expected result:

- the hard errors disappear
- the validator reports success or only the non-blocking `dom-6` warning

Presenter line:

> The important part here is not that the model free-styled a fix. It used the
> validator, used the Jira tool for context, then made concrete standards edits
> in the workspace and verified them by running the validator again.

### 7. Shelley closes the debugging loop

Shelley closes with:

> The example resources now validate cleanly enough for review, and both
> participants stayed synchronized in the same workspace topic throughout the
> debugging session.

## What This Demo Proves

- Shelley Manager can create a new isolated workspace on demand.
- The workspace runtime can be private while Manager stays the only public
  entrypoint.
- A browser user and a CLI user can collaborate in the same topic.
- A late joiner can catch up to an in-progress topic session.
- The workspace can host both:
  - a trusted local runtime tool reachable through bash
  - a first-class MCP stdio tool registered through the API
- The validator demo is grounded in real files and real validator behavior, not
  a fake profile compiler story.

## Exact Presenter Script

If the live demo needs tighter narration, use this sequence:

1. "Priya is working on the Acme RPM Implementation Guide and the blood pressure
   example resources are failing validation before tomorrow's review."
2. "She creates a fresh workspace called `bp-ig-fix` through Shelley Manager."
3. "Inside that workspace, Shelley runs the real FHIR Validator JAR against two
   example JSON resources and finds concrete bad values."
4. "Marco joins the same topic late from the CLI and catches up to the current
   session."
5. "Marco asks Shelley to search related HL7 Jira issues through an MCP stdio
   tool running inside the bubblewrapped workspace runtime."
6. "Shelley updates the JSON examples, re-runs validation, and clears the hard
   errors."
7. "The architectural split is that the validator is a trusted local runtime
   capability, while Jira search is a first-class MCP workspace tool."

## Must-Work Checklist Before Demo

- Manager web page:
  - create workspace
  - discover local tools from the manager-published catalog
  - optionally pre-register the Jira MCP tool
  - show share info
  - open proxied Shelley UI
- Topic realtime:
  - browser + CLI on same topic
  - late-join replay for active session
  - prompt/tool visibility on both clients
- Tools:
  - runtime availability of `fhir-validator` in the shared local tools mount
  - hosted registration of `hl7-jira` as MCP stdio
  - runtime availability of `bun` inside `bwrap`
  - workspace-local Bun MCP fixture can create/query its SQLite backing store
  - example JSON resources are present in the workspace and can be validated by
    the real validator CLI

## Optional Follow-On Demo

If we later want to return to a profile-authoring story, that should be a second
demo with SUSHI or IG Publisher in the loop. The real validator is a much
better fit for example-resource validation than for raw `.fsh`.
