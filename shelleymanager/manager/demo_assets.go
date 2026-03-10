package manager

import (
	"os"
	"path/filepath"
	"strings"
)

const demoRPMTemplateName = "acme-rpm-ig"

const demoPatientExampleJSON = `{
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
`

const demoObservationExampleJSON = `{
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
`

const demoWorkspaceReadme = `# Acme RPM IG Demo Workspace

This workspace simulates a blood-pressure implementation guide debugging session.

Known issue:
- two example resources fail the real FHIR Validator CLI
- the Patient uses a non-FHIR administrative gender code and an invalid birth date
- the blood pressure Observation is missing required panel components and has an invalid effectiveDateTime

Suggested first command:
- fhir-validator input/examples/Patient-bp-alice-smith.json input/examples/Observation-bp-alice-morning.json
`

func seedWorkspaceTemplate(workspaceDir, templateName string) error {
	switch strings.TrimSpace(templateName) {
	case "", demoRPMTemplateName:
	default:
		return nil
	}

	examplesDir := filepath.Join(workspaceDir, "input", "examples")
	if err := os.MkdirAll(examplesDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "README.md"), []byte(demoWorkspaceReadme), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(examplesDir, "Patient-bp-alice-smith.json"), []byte(demoPatientExampleJSON), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(examplesDir, "Observation-bp-alice-morning.json"), []byte(demoObservationExampleJSON), 0o644)
}
