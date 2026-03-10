# Demo Run Of Show: Fixing the Blood Pressure Panel in the Acme RPM IG

## Demo Goal

Show one believable collaborative standards workflow:

- Priya creates a fresh isolated workspace through Shelley Manager.
- She opens a shared topic in the browser and asks Shelley to debug a FHIR
  validator failure.
- Shelley uses a local non-MCP tool to run the FHIR Validator JAR inside the
  workspace.
- Marco joins late from the CLI, catches up to the in-progress topic, and asks
  Shelley to search related HL7 Jira issues through an MCP stdio tool.
- Shelley edits the profile, re-runs validation, and then pauses on a
  publish-preview action until Priya approves it.

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
- focus area for this demo: home blood pressure observations
- current release pressure: they want a preview build ready for tomorrow's
  work group review

The concrete bug is in the blood pressure panel profile:

- file: `input/fsh/BloodPressurePanel.fsh`
- generated artifact:
  `fsh-generated/resources/StructureDefinition-acme-bp-panel.json`
- symptom: the validator complains that the profile constrains systolic and
  diastolic `Observation.component` slices without declaring slicing metadata

## Fixed Demo Data

Use these exact demo values every time.

- workspace name: `bp-ig-fix`
- namespace: `acme`
- repo/template label in the create form: `acme-rpm-ig`
- topic name: `bp-panel-validator`

## Tools In The Demo

### `fhir-validator`

- kind: workspace-local executable tool
- protocol: not MCP
- implementation: wrapper script around the FHIR Validator JAR already present
  in the workspace image or mounted tool cache
- purpose: run IG validation and show concrete output against the generated
  StructureDefinition

### `hl7-jira`

- kind: MCP stdio workspace tool
- implementation: Bun fixture MCP server
- purpose: search a small fixture set of HL7 Jira issues
- tool name exposed through MCP: `jira.search`

Fixture results should include:

- `FHIR-39112`
  - title: `Validator flags Observation.component slicing without explicit discriminator metadata`
- `FHIR-40277`
  - title: `Clarify blood pressure component slicing examples in profiling guidance`

### `publish-preview`

- kind: workspace tool
- policy: static `approval_required`
- purpose: demonstrate a real pause-for-approval step before a sensitive action

## Starting Files

At demo start, `input/fsh/BloodPressurePanel.fsh` should look roughly like:

```fsh
Profile: AcmeBloodPressurePanel
Parent: Observation
Id: acme-bp-panel
Title: "Acme Blood Pressure Panel"
Description: "Panel profile for home blood pressure readings."

* status 1..1
* code = $LNC#85354-9 "Blood pressure panel with all children optional"
* subject 1..1
* effective[x] only dateTime
* component[systolic] 1..1
* component[systolic].code = $LNC#8480-6 "Systolic blood pressure"
* component[systolic].value[x] only Quantity
* component[diastolic] 1..1
* component[diastolic].code = $LNC#8462-4 "Diastolic blood pressure"
* component[diastolic].value[x] only Quantity
```

The important problem is that this constrains named slices, but does not
declare slicing on `component`.

## Representative Validator Output

The validator output does not need to be byte-for-byte real, but it should be
specific and clinically plausible. Use something in this shape:

```text
[ERROR] StructureDefinition/acme-bp-panel: Observation.component:
Slicing cannot be evaluated. The profile defines component[systolic] and
component[diastolic], but no slicing discriminator is declared on
Observation.component.
```

Shelley should summarize this in plain language:

> The profile is constraining systolic and diastolic components, but the
> generated StructureDefinition does not declare how those slices are
> discriminated. I should inspect `input/fsh/BloodPressurePanel.fsh` and add
> slicing metadata on `component`.

## Run Of Show

### 1. Priya creates the workspace

Priya opens Shelley Manager and fills in:

- Namespace: `acme`
- Workspace name: `bp-ig-fix`
- Template: `acme-rpm-ig`

She clicks `Create Workspace`.

The page should show:

- status: `running`
- `Open Workspace` button
- share card with:
  - browser URL:
    `https://demo.example.org/apis/v1/namespaces/acme/workspaces/bp-ig-fix`
  - CLI join command:

```bash
WS_MANAGER=https://demo.example.org bun run cli.ts connect bp-ig-fix bp-panel-validator
```

Presenter line:

> Priya is creating a fresh isolated workspace for one focused standards task.
> Shelley Manager is the only public endpoint. The Shelley runtime itself is
> private and running under bubblewrap.

### 2. Priya opens the shared topic in the browser

Priya clicks `Open Workspace`, then opens topic `bp-panel-validator`.

Her first prompt is exactly:

> Run the FHIR validator on this IG and explain why the blood pressure panel is failing.

The audience should understand:

- this is Shelley inside the workspace
- the browser is still talking through Shelley Manager
- topic state lives inside this one workspace runtime

### 3. Shelley runs the validator and finds the actual bug

Shelley invokes `fhir-validator`.

What the audience sees:

- a tool call for `fhir-validator`
- the concrete validator error above
- Shelley's explanation that the issue is missing slicing metadata on
  `Observation.component`

Presenter line:

> This first tool is deliberately not MCP. It is just a workspace-local
> executable tool around the validator JAR. We want to show that the workspace
> can host both plain executable tools and MCP-backed tools.

### 4. Marco joins late from the CLI and catches up

Marco arrives after the validator has already run.

He uses the share command:

```bash
WS_MANAGER=https://demo.example.org bun run cli.ts connect bp-ig-fix bp-panel-validator
```

What Marco should see immediately:

- the current topic connection
- replay of the recent topic events from the active session
- the validator tool call
- the validator error
- Shelley's current summary of the problem

Presenter line:

> Marco is joining late, but he is not joining blind. The topic websocket
> catches him up to the live session, so browser and CLI participants can share
> one in-progress debugging conversation.

### 5. Marco asks Shelley to search HL7 Jira

Marco types this exact prompt in the CLI:

> Search HL7 Jira for issues about Observation.component slicing and blood pressure profiles.

Shelley invokes the MCP stdio tool `hl7-jira`, specifically the MCP tool
`jira.search`.

The fixture returns:

- `FHIR-39112` — `Validator flags Observation.component slicing without explicit discriminator metadata`
- `FHIR-40277` — `Clarify blood pressure component slicing examples in profiling guidance`

Shelley summarizes:

> The Jira issues are consistent with the validator output. The likely local fix
> is still to declare slicing on `component` with a discriminator on `code`
> before constraining the systolic and diastolic slices.

Presenter line:

> This second tool is MCP over stdio. Same shared workspace, different tool
> transport, no change in how the participants collaborate.

### 6. Priya asks Shelley to fix the profile and re-run validation

Priya types this exact prompt in the browser:

> Update `input/fsh/BloodPressurePanel.fsh` to declare the systolic and diastolic component slices correctly, then run validation again.

Shelley edits the profile so the audience can see the fix in recognizable FSH.

The key added lines should be:

```fsh
* component ^slicing.discriminator.type = #pattern
* component ^slicing.discriminator.path = "code"
* component ^slicing.rules = #open
* component contains systolic 1..1 and diastolic 1..1
```

Shelley then re-runs `fhir-validator`.

Expected result:

- the slicing error disappears
- Shelley reports that the blood pressure panel now validates cleanly, or with
  only a minor warning unrelated to slicing

Presenter line:

> The important part here is not that the model free-styled a fix. It used the
> validator, used the Jira tool for context, then made a concrete standards edit
> in the workspace and verified it by running the validator again.

### 7. Priya asks Shelley to publish a preview

Priya types:

> Publish a preview build for tomorrow's work group review.

Shelley invokes `publish-preview`, but that tool has a static
`approval_required` policy.

What the audience sees:

- an approval request event on the topic
- browser and CLI both see it
- the tool does not continue yet

The approval request summary should read something like:

- tool: `publish-preview`
- action: `publish`
- summary: `Publish preview for bp-ig-fix after successful blood pressure panel validation`

### 8. Priya approves the publish

Priya clicks `Approve` in the browser.

What the audience sees:

- the approval request resolves
- Shelley resumes the paused tool call
- `publish-preview` completes successfully

Shelley closes with:

> The preview build is published for review. The blood pressure panel slicing
> error is fixed, validation is clean, and the workspace transcript captured the
> whole debugging session.

## What This Demo Proves

- Shelley Manager can create a new isolated workspace on demand.
- The workspace runtime can be private while Manager stays the only public
  entrypoint.
- A browser user and a CLI user can collaborate in the same topic.
- A late joiner can catch up to an in-progress topic session.
- The workspace can host both:
  - a plain local executable tool
  - an MCP stdio tool
- Static approval policy is enough to gate a sensitive action in a believable
  workflow.

## Exact Presenter Script

If the live demo needs tighter narration, use this sequence:

1. "Priya is working on the Acme RPM Implementation Guide and the blood pressure
   panel is failing validation before tomorrow's review."
2. "She creates a fresh workspace called `bp-ig-fix` through Shelley Manager."
3. "Inside that workspace, Shelley runs the FHIR Validator JAR and finds a
   concrete slicing problem in `input/fsh/BloodPressurePanel.fsh`."
4. "Marco joins the same topic late from the CLI and catches up to the current
   session."
5. "Marco asks Shelley to search related HL7 Jira issues through an MCP stdio
   tool."
6. "Shelley updates the FSH, re-runs validation, and clears the slicing error."
7. "When Priya asks Shelley to publish a preview, the workspace pauses on an
   approval request."
8. "Priya approves, the publish completes, and both participants have been
   watching the same shared topic the entire time."

## Must-Work Checklist Before Demo

- Manager web page:
  - create workspace
  - show share info
  - open proxied Shelley UI
- Topic realtime:
  - browser + CLI on same topic
  - late-join replay for active session
  - prompt/tool/approval visibility on both clients
- Tools:
  - hosted registration of `fhir-validator`
  - hosted registration of `hl7-jira` as MCP stdio
  - hosted registration of `publish-preview`
- Approval:
  - static `approval_required` grant on `publish-preview`
  - approve from browser
  - resume tool execution after approval
