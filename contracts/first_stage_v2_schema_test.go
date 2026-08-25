package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFirstStageV2SchemasPinFixedIdentity(t *testing.T) {
	for _, name := range []string{"first-stage-evidence-package-v2.schema.json", "first-stage-receipt-v2.schema.json", "windows-host-os-identity-record-v0.schema.json"} {
		data, err := os.ReadFile(filepath.Join(name))
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if document["$schema"] == nil || document["$id"] == nil {
			t.Fatalf("%s missing schema identity", name)
		}
	}
}

func TestFirstStageV2SchemasPinFixedCapabilityProviderAndArtifactSets(t *testing.T) {
	receipt := readSchemaDocument(t, "first-stage-receipt-v2.schema.json")
	if receipt["properties"].(map[string]any)["schema_version"].(map[string]any)["const"] != "1.2" {
		t.Fatal("receipt schema_version is not fixed to 1.2")
	}
	branches := receipt["allOf"].([]any)
	if len(branches) != 3 {
		t.Fatalf("receipt schema has %d capability ownership branches, want 3", len(branches))
	}
	expectedBranches := []struct {
		id, description, acquisition, sensitivity, provider, path, protection string
	}{
		{"PROCESS_IDENTITY_SNAPSHOT", "Capture a minimal snapshot of visible Windows process identities: process ID, parent process ID, and executable name.", "STATE_SNAPSHOT", "medium", "windows-toolhelp-process-snapshot", "derived/process-identity-snapshot.ndjson", "true"},
		{"WINDOWS_EVENT_LOG_SYSTEM_CHANNEL", "Capture the currently retained records from the local Windows System Event Log channel as one native EVTX export.", "EXISTING_ARTIFACT_EXPORT", "high", "windows-wevtapi-system-channel", "raw/windows-event-log-system.evtx", "true"},
		{"WINDOWS_HOST_OS_IDENTITY_SNAPSHOT", "Capture the local Windows host name, OS version/build, and native architecture.", "STATE_SNAPSHOT", "medium", "windows-native-host-os-identity", "derived/windows-host-os-identity.json", "false"},
	}
	for index, expected := range expectedBranches {
		branch := branches[index].(map[string]any)
		then := branch["then"].(map[string]any)
		properties := then["properties"].(map[string]any)
		request := properties["requested_capability"].(map[string]any)["properties"].(map[string]any)
		if request["protected"].(map[string]any)["const"].(bool) != (expected.protection == "true") {
			t.Fatalf("branch %d protection mismatch", index)
		}
		capabilityProperties := properties["capability"].(map[string]any)["properties"].(map[string]any)
		if capabilityProperties["id"].(map[string]any)["const"] != expected.id {
			t.Fatalf("branch %d capability id mismatch", index)
		}
		if capabilityProperties["description"].(map[string]any)["const"] != expected.description {
			t.Fatalf("branch %d capability description mismatch", index)
		}
		if capabilityProperties["acquisition_semantics"].(map[string]any)["const"] != expected.acquisition {
			t.Fatalf("branch %d capability acquisition mismatch", index)
		}
		if capabilityProperties["sensitivity"].(map[string]any)["const"] != expected.sensitivity {
			t.Fatalf("branch %d capability sensitivity mismatch", index)
		}
		providerProperties := properties["selected_provider"].(map[string]any)["properties"].(map[string]any)
		if providerProperties["id"].(map[string]any)["const"] != expected.provider {
			t.Fatalf("branch %d provider mismatch", index)
		}
		candidateEvaluations := properties["candidate_evaluations"].(map[string]any)
		candidateItems := candidateEvaluations["items"].(map[string]any)
		if candidateItems["$ref"] != "#/$defs/evaluation" {
			t.Fatalf("branch %d candidate evaluation does not use the trusted shared evaluation contract", index)
		}
		artifactProperties := properties["artifact_reference"].(map[string]any)["properties"].(map[string]any)
		if artifactProperties["path"].(map[string]any)["const"] != expected.path {
			t.Fatalf("branch %d artifact mismatch", index)
		}
	}
	defs := receipt["$defs"].(map[string]any)
	request := defs["request"].(map[string]any)
	requestProperties := request["properties"].(map[string]any)
	ids := requestProperties["id"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "receipt capability IDs", ids, []string{"PROCESS_IDENTITY_SNAPSHOT", "WINDOWS_EVENT_LOG_SYSTEM_CHANNEL", "WINDOWS_HOST_OS_IDENTITY_SNAPSHOT"})
	providers := defs["provider"].(map[string]any)["properties"].(map[string]any)["id"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "receipt provider IDs", providers, []string{"windows-toolhelp-process-snapshot", "windows-wevtapi-system-channel", "windows-native-host-os-identity"})
	evaluation := defs["evaluation"].(map[string]any)
	evaluationProperties := evaluation["properties"].(map[string]any)
	evaluationProviders := evaluationProperties["provider_id"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "candidate evaluation provider IDs", evaluationProviders, []string{"windows-toolhelp-process-snapshot", "windows-wevtapi-system-channel", "windows-native-host-os-identity"})
	for _, value := range evaluationProviders {
		if value == "provider-x" {
			t.Fatal("arbitrary candidate evaluation Provider ID is present in the schema enum")
		}
	}
	reasons := receipt["properties"].(map[string]any)["orchestration_reason"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "receipt orchestration reasons", reasons, []string{"", "UNKNOWN_CAPABILITY", "RESOLUTION_FAILED", "CANCELLED", "PACKAGE_FINALIZATION_FAILED"})
	artifacts := defs["artifact"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "receipt artifact paths", artifacts, []string{"derived/process-identity-snapshot.ndjson", "raw/windows-event-log-system.evtx", "derived/windows-host-os-identity.json"})

	packageSchema := readSchemaDocument(t, "first-stage-evidence-package-v2.schema.json")
	packageProperties := packageSchema["properties"].(map[string]any)
	if packageProperties["runtime_artifact"].(map[string]any)["const"] != "tannang-first-stage-multi-v1.2" {
		t.Fatal("package runtime artifact is not fixed to v1.2")
	}
	receipts := packageProperties["receipt_references"].(map[string]any)["items"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "package receipt references", receipts, []string{"receipts/PROCESS_IDENTITY_SNAPSHOT.json", "receipts/WINDOWS_EVENT_LOG_SYSTEM_CHANNEL.json", "receipts/WINDOWS_HOST_OS_IDENTITY_SNAPSHOT.json"})
}

func readSchemaDocument(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(name))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return document
}

func assertExactStrings(t *testing.T, label string, values []any, expected []string) {
	t.Helper()
	if len(values) != len(expected) {
		t.Fatalf("%s count=%d want=%d", label, len(values), len(expected))
	}
	for index, value := range values {
		if value != expected[index] {
			t.Fatalf("%s[%d]=%v want=%s", label, index, value, expected[index])
		}
	}
}
