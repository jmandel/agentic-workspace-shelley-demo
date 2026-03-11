import { Link } from "wouter";

export function TutorialPage() {
  return (
    <div className="page page-narrow">
      <div className="card">
        <div className="row row-between" style={{ alignItems: "flex-start" }}>
          <div>
            <h1>WS Language Tutorial</h1>
            <p className="muted">
              Use <code>ws ...</code> prompts with the predictable model to
              script live demo behavior on the fly. Tags can appear in any
              order; only one primary action is allowed per prompt.
            </p>
          </div>
          <Link href="/" className="btn btn-secondary">
            Back
          </Link>
        </div>
      </div>

      <div className="grid-2" style={{ marginTop: 16 }}>
        <section className="card">
          <h2>Primary Actions</h2>
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            <li>
              <code>text</code> or <code>echo</code>: return assistant text
              immediately.
            </li>
            <li>
              <code>bash</code>: call Shelley's built-in <code>bash</code> tool
              with your command.
            </li>
            <li>
              <code>validator</code>: run the local{" "}
              <code>fhir-validator</code> wrapper through <code>bash</code>.
            </li>
            <li>
              <code>publisher</code>: run the local <code>ig-publisher</code>{" "}
              wrapper through <code>bash</code>.
            </li>
            <li>
              <code>jira</code>: call the first-class <code>hl7-jira</code> MCP
              tool as <code>jira.search</code>.
            </li>
            <li>
              <code>tool</code> + <code>action</code> + optional{" "}
              <code>input</code>: call any registered workspace tool
              explicitly, including <code>jira.read</code>.
            </li>
          </ul>
        </section>

        <section className="card">
          <h2>Timing Tags</h2>
          <ul style={{ margin: 0, paddingLeft: 18 }}>
            <li>
              <code>pause2</code> or <code>pause 2</code>: delay before the
              assistant responds or starts a tool.
            </li>
            <li>
              <code>toolpause3</code> or <code>toolpause 3</code>: keep the
              tool busy for 3 seconds. Easiest way to demonstrate queueing.
            </li>
            <li>
              <code>afterpause1</code>: delay the follow-up assistant text
              after a tool result arrives.
            </li>
            <li>
              <code>aftertext "..."</code>: customize what the predictable model
              says after the tool call finishes.
            </li>
          </ul>
        </section>
      </div>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>Demo-Ready Examples</h2>
        <div className="stack-sm">
          <pre>
            ws text "Thanks. Let me summarize the validator findings."
          </pre>
          <pre>
            ws pause2 validator
            "input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json"
            toolpause3 aftertext "The validator found bad patient demographics
            and a broken blood pressure example."
          </pre>
          <pre>
            ws jira "FHIR validator invalid dates bad codes blood pressure"
            pause1
          </pre>
          <pre>
            {`ws tool hl7-jira action jira.search input '{"query":"validator warning blood pressure slicing"}' aftertext "I found two relevant HL7 Jira threads."`}
          </pre>
          <pre>
            {`ws tool hl7-jira action jira.read input '{"key":"FHIR-20482"}' aftertext "This issue shows the full stored Jira record."`}
          </pre>
          <pre>
            {`ws bash "sed -n '1,200p' input/examples/Patient-bp-alice-smith.json && printf '\\n---\\n' && sed -n '1,240p' input/examples/Observation-bp-alice-morning.json"`}
          </pre>
        </div>
      </section>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>Whole Demo Commands</h2>
        <pre>{`1. ws validator "input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json" toolpause5 aftertext "The validator found bad patient demographics and a broken blood pressure example."
2. ws jira "FHIR validator invalid dates bad codes blood pressure" pause1
3. ws tool hl7-jira action jira.read input '{"key":"FHIR-20482"}' aftertext "This issue shows the full stored Jira record."
4. ws bash "python - <<'PY'
from pathlib import Path
import json
patient = Path('input/examples/Patient-bp-alice-smith.json')
data = json.loads(patient.read_text())
data['gender'] = 'female'
data['birthDate'] = '1974-12-25'
patient.write_text(json.dumps(data, indent=2) + '\\n')
print('Fixed Patient gender and birthDate.')
PY"
5. ws validator "input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json" aftertext "The patient example is fixed; the blood pressure example still needs component slices."
6. ws text "Marco, can you review the remaining Observation issues before we publish the preview?"`}</pre>
      </section>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>Queueing Trick</h2>
        <p>
          To show queueing live in the demo, make the current turn visibly slow
          at the exact point you want:
        </p>
        <pre>
          ws validator
          "input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json"
          toolpause5 aftertext "Validator run finished."
        </pre>
        <p className="muted">
          While that five-second validator step is running, submit another prompt
          from the browser or CLI. The second prompt will queue, and the queue
          panel will let you edit, reorder, or delete it before it runs.
        </p>
      </section>

      <section className="card" style={{ marginTop: 16 }}>
        <h2>Rules Of Thumb</h2>
        <ul style={{ margin: 0, paddingLeft: 18 }}>
          <li>Wrap multi-word values in single or double quotes.</li>
          <li>
            <code>input</code> must be valid JSON.
          </li>
          <li>Use only one primary action in a single prompt.</li>
          <li>
            <code>ws help</code> in a predictable-model chat returns a compact
            version of this tutorial.
          </li>
        </ul>
      </section>
    </div>
  );
}
