package manager

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"
)

const demoRPMTemplateName = "acme-rpm-ig"

const demoBloodPressurePanelFSH = `Profile: AcmeBloodPressurePanel
Parent: Observation
Id: acme-bp-panel
Title: "Acme Blood Pressure Panel"
Description: "Deliberately broken demo profile for validator debugging."

* status = #final
* code = http://loinc.org#85354-9 "Blood pressure panel with all children optional"
* subject 1..1
* effective[x] only dateTime
* component contains
    systolicBP 1..1 and
    diastolicBP 1..1
* component[systolicBP].code = http://loinc.org#8480-6 "Systolic blood pressure"
* component[systolicBP].value[x] only Quantity
* component[diastolicBP].code = http://loinc.org#8462-4 "Diastolic blood pressure"
* component[diastolicBP].value[x] only Quantity
`

const demoWorkspaceReadme = `# Acme RPM IG Demo Workspace

This workspace simulates a blood-pressure implementation guide debugging session.

Known issue:
- input/fsh/BloodPressurePanel.fsh constrains Observation.component slices
  without declaring slicing metadata.

Suggested first command:
- fhir-validator input/fsh/BloodPressurePanel.fsh
`

//go:embed testdata/hl7-jira-mcp.js
var demoHL7JiraMCPFixtureScript string

func seedWorkspaceTemplate(workspaceDir, templateName string) error {
	switch strings.TrimSpace(templateName) {
	case "", demoRPMTemplateName:
	default:
		return nil
	}

	if err := os.MkdirAll(filepath.Join(workspaceDir, "input", "fsh"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "README.md"), []byte(demoWorkspaceReadme), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workspaceDir, "input", "fsh", "BloodPressurePanel.fsh"), []byte(demoBloodPressurePanelFSH), 0o644)
}
