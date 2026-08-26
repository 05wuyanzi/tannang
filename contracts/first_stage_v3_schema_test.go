package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFirstStageV3SchemasParseAndPinNetworkIdentity(t *testing.T) {
	for _, name := range []string{"first-stage-evidence-package-v3.schema.json", "first-stage-receipt-v3.schema.json", "windows-transport-endpoint-record-v0.schema.json"} {
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
	record := readSchemaDocument(t, "windows-transport-endpoint-record-v0.schema.json")
	if record["additionalProperties"] != false {
		t.Fatal("transport record schema must reject additional properties")
	}
	required := record["required"].([]any)
	assertExactStrings(t, "transport record fields", required, []string{"protocol", "address_family", "local_address", "local_port", "local_scope_id", "remote_address", "remote_port", "remote_scope_id", "tcp_state", "owning_pid"})
	receipt := readSchemaDocument(t, "first-stage-receipt-v3.schema.json")
	request := receipt["$defs"].(map[string]any)["request"].(map[string]any)["properties"].(map[string]any)["id"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "v1.3 capability IDs", request, []string{"PROCESS_IDENTITY_SNAPSHOT", "WINDOWS_EVENT_LOG_SYSTEM_CHANNEL", "WINDOWS_HOST_OS_IDENTITY_SNAPSHOT", "WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT"})
	providers := receipt["$defs"].(map[string]any)["provider"].(map[string]any)["properties"].(map[string]any)["id"].(map[string]any)["enum"].([]any)
	assertExactStrings(t, "v1.3 provider IDs", providers, []string{"windows-toolhelp-process-snapshot", "windows-wevtapi-system-channel", "windows-native-host-os-identity", "windows-iphlpapi-transport-endpoints"})
	expectedProviders := map[string]string{
		"PROCESS_IDENTITY_SNAPSHOT":           "windows-toolhelp-process-snapshot",
		"WINDOWS_EVENT_LOG_SYSTEM_CHANNEL":    "windows-wevtapi-system-channel",
		"WINDOWS_HOST_OS_IDENTITY_SNAPSHOT":   "windows-native-host-os-identity",
		"WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT": "windows-iphlpapi-transport-endpoints",
	}
	branches, ok := receipt["allOf"].([]any)
	if !ok {
		t.Fatal("v1.3 receipt schema allOf is missing")
	}
	seen := make(map[string]bool, len(expectedProviders))
	for _, rawBranch := range branches {
		branch, ok := rawBranch.(map[string]any)
		if !ok {
			t.Fatal("v1.3 receipt schema contains a malformed capability branch")
		}
		condition := branch["if"].(map[string]any)
		conditionProperties := condition["properties"].(map[string]any)
		conditionRequest := conditionProperties["requested_capability"].(map[string]any)
		conditionRequestProperties := conditionRequest["properties"].(map[string]any)
		id := conditionRequestProperties["id"].(map[string]any)["const"].(string)
		want, expected := expectedProviders[id]
		if !expected {
			continue
		}
		then := branch["then"].(map[string]any)
		thenProperties := then["properties"].(map[string]any)
		evaluations := thenProperties["candidate_evaluations"].(map[string]any)
		items := evaluations["items"].(map[string]any)
		itemProperties := items["properties"].(map[string]any)
		providerConstraint := itemProperties["provider_id"].(map[string]any)
		got, ok := providerConstraint["const"].(string)
		if !ok || got != want {
			t.Fatalf("v1.3 %s candidate evaluation provider const=%v want %q", id, providerConstraint["const"], want)
		}
		seen[id] = true
	}
	for id := range expectedProviders {
		if !seen[id] {
			t.Fatalf("v1.3 schema has no capability-specific candidate evaluation rule for %s", id)
		}
	}
	packageSchema := readSchemaDocument(t, "first-stage-evidence-package-v3.schema.json")
	if packageSchema["properties"].(map[string]any)["runtime_artifact"].(map[string]any)["const"] != "tannang-first-stage-multi-v1.3" {
		t.Fatal("v1.3 package runtime artifact mismatch")
	}
}
