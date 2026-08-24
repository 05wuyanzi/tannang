package contracts_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/receipt"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

// These tests intentionally do not implement a general JSON Schema engine.
// They pin the bounded v1.1 branch structure and exercise the same runtime
// validators that serialize the two known real capability states.
func TestFirstStageV1SchemaBindsCapabilityOwnership(t *testing.T) {
	receiptSchema := readSchema(t, "first-stage-receipt-v1.schema.json")
	packageSchema := readSchema(t, "first-stage-evidence-package-v1.schema.json")

	if receiptSchema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || packageSchema["$schema"] != receiptSchema["$schema"] {
		t.Fatal("v1 schemas must remain Draft 2020-12")
	}
	if receiptSchema["$defs"] == nil || packageSchema["$defs"] == nil {
		t.Fatal("v1 schemas must expose bounded definitions")
	}
	receiptDefs := receiptSchema["$defs"].(map[string]any)
	for _, name := range []string{"process_capability", "eventlog_capability", "process_provider", "eventlog_provider", "process_artifact", "eventlog_artifact"} {
		if _, ok := receiptDefs[name]; !ok {
			t.Fatalf("receipt schema is missing bounded definition %q", name)
		}
	}
	assertConstAt(t, receiptDefs, "process_capability", "id", capability.ProcessIdentitySnapshotID)
	assertConstAt(t, receiptDefs, "process_capability", "acquisition_semantics", string(capability.StateSnapshot))
	assertConstAt(t, receiptDefs, "eventlog_capability", "id", capability.WindowsEventLogSystemChannelID)
	assertConstAt(t, receiptDefs, "eventlog_capability", "acquisition_semantics", string(capability.ExistingArtifactExport))
	assertConstAt(t, receiptDefs, "process_provider", "id", "windows-toolhelp-process-snapshot")
	assertConstAt(t, receiptDefs, "eventlog_provider", "id", "windows-wevtapi-system-channel")
	assertConstAt(t, receiptDefs, "process_artifact", "path", receipt.FirstStageArtifactPath)
	assertConstAt(t, receiptDefs, "process_artifact", "raw_or_derived", "DERIVED")
	assertConstAt(t, receiptDefs, "eventlog_artifact", "path", receipt.WindowsEventLogSystemArtifactPath)
	assertConstAt(t, receiptDefs, "eventlog_artifact", "raw_or_derived", "RAW")

	assertReceiptSchemaComposition(t, receiptSchema, receiptDefs)
	assertPackageSchemaComposition(t, packageSchema)
}

func assertReceiptSchemaComposition(t *testing.T, schema map[string]any, defs map[string]any) {
	t.Helper()
	allOf := schemaArray(t, schema, "allOf")
	if len(allOf) < 4 {
		t.Fatalf("receipt schema lacks capability/execution conditional branches: %#v", schema["allOf"])
	}

	capabilityBranches := schemaObject(t, allOf[0], "receipt allOf[0]")
	oneOf := schemaArray(t, capabilityBranches, "oneOf")
	if len(oneOf) != 2 {
		t.Fatalf("receipt capability branch count changed: got %d", len(oneOf))
	}
	assertRequestedCapabilityBranch(t, oneOf[0], capability.ProcessIdentitySnapshotID)
	assertRequestedCapabilityBranch(t, oneOf[1], capability.WindowsEventLogSystemChannelID)

	processConditional := findCapabilityConditional(t, allOf, capability.ProcessIdentitySnapshotID)
	assertRequired(t, schemaObject(t, processConditional["if"], "process capability if"), "requested_capability")
	processIfRequest := schemaObject(t, schemaObject(t, schemaObject(t, processConditional["if"], "process capability if")["properties"], "process if properties")["requested_capability"], "process if request")
	assertRequired(t, processIfRequest, "id")
	assertConst(t, schemaObject(t, schemaObject(t, processIfRequest["properties"], "process if request properties")["id"], "process if id"), "const", capability.ProcessIdentitySnapshotID)
	processThen := schemaObject(t, processConditional["then"], "process capability then")
	processThenProperties := schemaObject(t, processThen["properties"], "process then properties")
	assertPropertyRef(t, processThenProperties, "capability", "#/$defs/process_capability")
	assertPropertyRef(t, processThenProperties, "selected_provider", "#/$defs/process_provider")
	assertPropertyItemsRef(t, processThenProperties, "candidate_evaluations", "#/$defs/process_evaluation")
	assertPropertyRef(t, processThenProperties, "artifact_reference", "#/$defs/process_artifact")

	// The bounded schema uses the process capability as the if branch and the
	// fixed Event Log capability as the else branch; the receipt oneOf above
	// prevents any third requested capability from reaching this else path.
	eventElse := schemaObject(t, processConditional["else"], "Event Log capability else")
	eventElseProperties := schemaObject(t, eventElse["properties"], "Event Log else properties")
	assertPropertyConst(t, eventElseProperties, "requested_capability", "protected", false)
	assertPropertyRef(t, eventElseProperties, "capability", "#/$defs/eventlog_capability")
	assertPropertyRef(t, eventElseProperties, "selected_provider", "#/$defs/eventlog_provider")
	assertPropertyItemsRef(t, eventElseProperties, "candidate_evaluations", "#/$defs/eventlog_evaluation")
	assertPropertyRef(t, eventElseProperties, "artifact_reference", "#/$defs/eventlog_artifact")

	assertDefinitionIdentity(t, defs, "provider", map[string]any{"class": "FIRST_PARTY_NATIVE"})
	assertDefinitionIdentity(t, defs, "process_provider", map[string]any{"id": "windows-toolhelp-process-snapshot"})
	assertDefinitionIdentity(t, defs, "eventlog_provider", map[string]any{"id": "windows-wevtapi-system-channel"})
	assertDefinitionIdentity(t, defs, "process_artifact", map[string]any{
		"path":              "derived/process-identity-snapshot.ndjson",
		"media_type":        "application/x-ndjson",
		"content_schema_id": "https://github.com/05wuyanzi/tannang/contracts/process-identity-snapshot-record-v0.schema.json",
		"raw_or_derived":    "DERIVED",
	})
	assertDefinitionIdentity(t, defs, "eventlog_artifact", map[string]any{
		"path":              "raw/windows-event-log-system.evtx",
		"media_type":        "application/x-evtx",
		"content_schema_id": "urn:tannang:artifact:windows-event-log-system-evtx-v0",
		"raw_or_derived":    "RAW",
	})

	selectedProviderConditional := findRequiredConditional(t, allOf, "selected_provider")
	selectedProviderThen := schemaObject(t, selectedProviderConditional["then"], "selected provider then")
	selectedProviderThenProperties := schemaObject(t, selectedProviderThen["properties"], "selected provider then properties")
	assertRequired(t, selectedProviderThen, "compatibility_reason")
	assertRequired(t, selectedProviderThen, "candidate_evaluations")
	assertPropertyNumber(t, selectedProviderThenProperties, "candidate_evaluations", "minItems", 1)

	artifactConditional := findRequiredConditional(t, allOf, "artifact_reference")
	artifactThen := schemaObject(t, artifactConditional["then"], "retained artifact then")
	artifactThenProperties := schemaObject(t, artifactThen["properties"], "retained artifact then properties")
	assertPropertyConst(t, artifactThenProperties, "attempted", "const", true)
	assertPropertyEnum(t, artifactThenProperties, "execution", "state", []any{"COLLECTED", "PARTIAL"})
	assertRequired(t, schemaObject(t, artifactThenProperties["execution"], "retained execution"), "state")

	retainedExecutionConditional := findExecutionStateConditional(t, allOf)
	retainedExecutionThen := schemaObject(t, retainedExecutionConditional["then"], "retained execution then")
	assertRequired(t, retainedExecutionThen, "artifact_reference")

	orchestrationConditional := findRequiredConditional(t, allOf, "orchestration_reason")
	orchestrationThen := schemaObject(t, orchestrationConditional["then"], "orchestration then")
	orchestrationThenProperties := schemaObject(t, orchestrationThen["properties"], "orchestration then properties")
	assertPropertyConst(t, orchestrationThenProperties, "attempted", "const", false)
	assertPropertyConst(t, orchestrationThenProperties, "execution", "reason", "NONE")

	cancelledConditional := findConstConditional(t, allOf, "orchestration_reason", "CANCELLED")
	assertRequired(t, schemaObject(t, cancelledConditional["if"], "cancelled if"), "orchestration_reason")
	cancelledThen := schemaObject(t, cancelledConditional["then"], "cancelled then")
	assertNotRequired(t, cancelledThen, "artifact_reference")
}

func assertPackageSchemaComposition(t *testing.T, schema map[string]any) {
	t.Helper()
	properties := schemaObject(t, schema["properties"], "package properties")
	receipts := schemaObject(t, properties["receipt_references"], "package receipt references")
	assertNumber(t, receipts, "minItems", 2)
	assertNumber(t, receipts, "maxItems", 2)
	assertBool(t, receipts, "uniqueItems", true)
	assertEnumSet(t, schemaObject(t, receipts["items"], "package receipt items"), "enum", []any{
		"receipts/PROCESS_IDENTITY_SNAPSHOT.json",
		"receipts/WINDOWS_EVENT_LOG_SYSTEM_CHANNEL.json",
	})
	assertArrayContainsConstGuard(t, receipts, "receipts/PROCESS_IDENTITY_SNAPSHOT.json")
	assertArrayContainsConstGuard(t, receipts, "receipts/WINDOWS_EVENT_LOG_SYSTEM_CHANNEL.json")

	artifacts := schemaObject(t, properties["artifact_references"], "package artifact references")
	assertNumber(t, artifacts, "maxItems", 2)
	assertBool(t, artifacts, "uniqueItems", true)
	assertRef(t, schemaObject(t, artifacts["items"], "package artifact items"), "$ref", "#/$defs/artifact")
	assertArrayContainsPathGuard(t, artifacts, "derived/process-identity-snapshot.ndjson")
	assertArrayContainsPathGuard(t, artifacts, "raw/windows-event-log-system.evtx")

	artifactDef := schemaObject(t, schemaObject(t, schema["$defs"], "package defs")["artifact"], "package artifact definition")
	oneOf := schemaArray(t, artifactDef, "oneOf")
	if len(oneOf) != 2 {
		t.Fatalf("package artifact ownership must have exactly two branches, got %d", len(oneOf))
	}
	assertInlineArtifactBranch(t, oneOf[0], "derived/process-identity-snapshot.ndjson", "application/x-ndjson", "https://github.com/05wuyanzi/tannang/contracts/process-identity-snapshot-record-v0.schema.json", "DERIVED")
	assertInlineArtifactBranch(t, oneOf[1], "raw/windows-event-log-system.evtx", "application/x-evtx", "urn:tannang:artifact:windows-event-log-system-evtx-v0", "RAW")
}

func schemaObject(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s must be an object, got %T", label, value)
	}
	return object
}

func schemaArray(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	array, ok := object[key].([]any)
	if !ok {
		t.Fatalf("schema key %s must be an array, got %T", key, object[key])
	}
	return array
}

func assertRequestedCapabilityBranch(t *testing.T, branch any, capabilityID string) {
	t.Helper()
	object := schemaObject(t, branch, "receipt capability branch")
	properties := schemaObject(t, object["properties"], "receipt capability branch properties")
	request := schemaObject(t, properties["requested_capability"], "receipt capability branch request")
	assertRequired(t, object, "requested_capability")
	requestProperties := schemaObject(t, request["properties"], "receipt capability branch request properties")
	assertConst(t, schemaObject(t, requestProperties["id"], "receipt capability branch request id"), "const", capabilityID)
	assertRequired(t, request, "id")
}

func findCapabilityConditional(t *testing.T, allOf []any, capabilityID string) map[string]any {
	t.Helper()
	for _, value := range allOf {
		conditional, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ifObject, ok := conditional["if"].(map[string]any)
		if !ok {
			continue
		}
		if capabilityConst(ifObject) == capabilityID {
			return conditional
		}
	}
	t.Fatalf("receipt schema has no capability conditional for %q", capabilityID)
	return nil
}

func capabilityConst(ifObject map[string]any) string {
	properties, ok := ifObject["properties"].(map[string]any)
	if !ok {
		return ""
	}
	request, ok := properties["requested_capability"].(map[string]any)
	if !ok {
		return ""
	}
	requestProperties, ok := request["properties"].(map[string]any)
	if !ok {
		return ""
	}
	idSchema, ok := requestProperties["id"].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := idSchema["const"].(string)
	return id
}

func findRequiredConditional(t *testing.T, allOf []any, requiredName string) map[string]any {
	t.Helper()
	for _, value := range allOf {
		conditional, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ifObject, ok := conditional["if"].(map[string]any)
		if !ok || !hasRequired(ifObject, requiredName) {
			continue
		}
		return conditional
	}
	t.Fatalf("receipt schema has no conditional requiring %q", requiredName)
	return nil
}

func findConstConditional(t *testing.T, allOf []any, property, want string) map[string]any {
	t.Helper()
	for _, value := range allOf {
		conditional, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ifObject, ok := conditional["if"].(map[string]any)
		if !ok {
			continue
		}
		properties, ok := ifObject["properties"].(map[string]any)
		if !ok {
			continue
		}
		propertySchema, ok := properties[property].(map[string]any)
		if !ok || propertySchema["const"] != want {
			continue
		}
		return conditional
	}
	t.Fatalf("receipt schema has no %s const conditional for %q", property, want)
	return nil
}

func findExecutionStateConditional(t *testing.T, allOf []any) map[string]any {
	t.Helper()
	for _, value := range allOf {
		conditional, ok := value.(map[string]any)
		if !ok {
			continue
		}
		ifObject, ok := conditional["if"].(map[string]any)
		if !ok {
			continue
		}
		properties, ok := ifObject["properties"].(map[string]any)
		if !ok {
			continue
		}
		executionSchema, ok := properties["execution"].(map[string]any)
		if !ok {
			continue
		}
		executionProperties, ok := executionSchema["properties"].(map[string]any)
		if !ok {
			continue
		}
		stateSchema, ok := executionProperties["state"].(map[string]any)
		if !ok || !sameValues(stateSchema["enum"], []any{"COLLECTED", "PARTIAL"}) {
			continue
		}
		return conditional
	}
	t.Fatal("receipt schema has no retained execution conditional")
	return nil
}

func assertDefinitionIdentity(t *testing.T, defs map[string]any, definition string, expected map[string]any) {
	t.Helper()
	value := schemaObject(t, defs[definition], "definition "+definition)
	for property, want := range expected {
		if definitionConst(value, property) != want {
			t.Fatalf("definition %s does not bind %s to %#v", definition, property, want)
		}
	}
}

func definitionConst(definition map[string]any, property string) any {
	if properties, ok := definition["properties"].(map[string]any); ok {
		if propertySchema, ok := properties[property].(map[string]any); ok {
			if value, ok := propertySchema["const"]; ok {
				return value
			}
		}
	}
	if branches, ok := definition["allOf"].([]any); ok {
		for _, branch := range branches {
			branchObject, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			if value := definitionConst(branchObject, property); value != nil {
				return value
			}
		}
	}
	return nil
}

func assertPropertyRef(t *testing.T, properties map[string]any, property, want string) {
	t.Helper()
	assertRef(t, schemaObject(t, properties[property], "property "+property), "$ref", want)
}

func assertPropertyItemsRef(t *testing.T, properties map[string]any, property, want string) {
	t.Helper()
	propertySchema := schemaObject(t, properties[property], "property "+property)
	assertRef(t, schemaObject(t, propertySchema["items"], property+" items"), "$ref", want)
}

func assertPropertyConst(t *testing.T, properties map[string]any, property, nestedKey string, want any) {
	t.Helper()
	propertySchema := schemaObject(t, properties[property], "property "+property)
	if nestedKey == "const" {
		assertConst(t, propertySchema, "const", want)
		return
	}
	nestedProperties := schemaObject(t, propertySchema["properties"], property+" properties")
	assertConst(t, schemaObject(t, nestedProperties[nestedKey], property+"."+nestedKey), "const", want)
}

func assertPropertyEnum(t *testing.T, properties map[string]any, property, nestedKey string, want []any) {
	t.Helper()
	propertySchema := schemaObject(t, properties[property], "property "+property)
	nestedProperties := schemaObject(t, propertySchema["properties"], property+" properties")
	assertEnumSet(t, schemaObject(t, nestedProperties[nestedKey], property+"."+nestedKey), "enum", want)
}

func assertPropertyNumber(t *testing.T, properties map[string]any, property, key string, want float64) {
	t.Helper()
	assertNumber(t, schemaObject(t, properties[property], "property "+property), key, want)
}

func assertInlineArtifactBranch(t *testing.T, branch any, path, mediaType, schemaID, rawOrDerived string) {
	t.Helper()
	properties := schemaObject(t, schemaObject(t, branch, "inline artifact branch")["properties"], "inline artifact properties")
	for property, want := range map[string]any{"path": path, "media_type": mediaType, "content_schema_id": schemaID, "raw_or_derived": rawOrDerived} {
		assertConst(t, schemaObject(t, properties[property], "inline artifact "+property), "const", want)
	}
}

func assertArrayContainsConstGuard(t *testing.T, arraySchema map[string]any, want string) {
	t.Helper()
	for _, value := range schemaArray(t, arraySchema, "allOf") {
		guard := schemaObject(t, value, "array contains guard")
		contains := schemaObject(t, guard["contains"], "array contains")
		if contains["const"] != want {
			continue
		}
		assertNumber(t, guard, "minContains", 1)
		assertNumber(t, guard, "maxContains", 1)
		return
	}
	t.Fatalf("array is missing exact contains guard for %q", want)
}

func assertArrayContainsPathGuard(t *testing.T, arraySchema map[string]any, want string) {
	t.Helper()
	for _, value := range schemaArray(t, arraySchema, "allOf") {
		guard := schemaObject(t, value, "artifact ownership guard")
		ifObject := schemaObject(t, guard["if"], "artifact ownership if")
		thenObject := schemaObject(t, guard["then"], "artifact ownership then")
		if !containsPath(ifObject["contains"], want) || !containsPath(thenObject["contains"], want) {
			continue
		}
		assertNumber(t, thenObject, "minContains", 1)
		assertNumber(t, thenObject, "maxContains", 1)
		return
	}
	t.Fatalf("artifact references are missing exact ownership guard for %q", want)
}

func containsPath(value any, want string) bool {
	contains, ok := value.(map[string]any)
	if !ok {
		return false
	}
	properties, ok := contains["properties"].(map[string]any)
	if !ok {
		return false
	}
	pathSchema, ok := properties["path"].(map[string]any)
	return ok && pathSchema["const"] == want
}

func assertNotRequired(t *testing.T, object map[string]any, name string) {
	t.Helper()
	notObject := schemaObject(t, object["not"], "not schema")
	if !hasRequired(notObject, name) {
		t.Fatalf("not schema must forbid required property %q", name)
	}
}

func assertRequired(t *testing.T, object map[string]any, name string) {
	t.Helper()
	if !hasRequired(object, name) {
		t.Fatalf("schema must require property %q", name)
	}
}

func hasRequired(object map[string]any, name string) bool {
	required, ok := object["required"].([]any)
	if !ok {
		return false
	}
	for _, value := range required {
		if value == name {
			return true
		}
	}
	return false
}

func assertConst(t *testing.T, object map[string]any, key string, want any) {
	t.Helper()
	if object[key] != want {
		t.Fatalf("schema key %s = %#v, want %#v", key, object[key], want)
	}
}

func assertRef(t *testing.T, object map[string]any, key, want string) {
	t.Helper()
	assertConst(t, object, key, want)
}

func assertNumber(t *testing.T, object map[string]any, key string, want float64) {
	t.Helper()
	value, ok := object[key].(float64)
	if !ok || value != want {
		t.Fatalf("schema number %s = %#v, want %v", key, object[key], want)
	}
}

func assertBool(t *testing.T, object map[string]any, key string, want bool) {
	t.Helper()
	value, ok := object[key].(bool)
	if !ok || value != want {
		t.Fatalf("schema boolean %s = %#v, want %v", key, object[key], want)
	}
}

func assertEnumSet(t *testing.T, object map[string]any, key string, want []any) {
	t.Helper()
	if !sameValues(object[key], want) {
		t.Fatalf("schema enum %s = %#v, want %#v", key, object[key], want)
	}
}

func sameValues(actual, want any) bool {
	actualArray, actualOK := actual.([]any)
	wantArray, wantOK := want.([]any)
	if !actualOK || !wantOK || len(actualArray) != len(wantArray) {
		return false
	}
	for index := range wantArray {
		if actualArray[index] != wantArray[index] {
			return false
		}
	}
	return true
}

func TestFirstStageV1RuntimeParityFixtures(t *testing.T) {
	process := validMultiProcessReceipt()
	event := validMultiEventReceipt()

	valid := map[string]receipt.FirstStageRecord{
		"process collected": process,
		"event collected":   event,
		"event unavailable": eventUnavailableReceipt(),
		"event cancelled":   eventCancelledReceipt(),
	}
	for name, record := range valid {
		if err := record.Validate(); err != nil {
			t.Errorf("valid %s fixture rejected by runtime: %v", name, err)
		}
	}

	invalid := map[string]receipt.FirstStageRecord{}
	wrongProcessProvider := process
	wrongProcessProvider.SelectedProvider = &receipt.ProviderIdentity{ID: "windows-wevtapi-system-channel", Class: provider.FirstPartyNative}
	invalid["process with Event Log provider"] = wrongProcessProvider
	wrongEventProvider := event
	wrongEventProvider.SelectedProvider = &receipt.ProviderIdentity{ID: "windows-toolhelp-process-snapshot", Class: provider.FirstPartyNative}
	invalid["Event Log with process provider"] = wrongEventProvider
	wrongProcessArtifact := process
	wrongProcessArtifact.ArtifactReference = &receipt.ArtifactReference{Path: receipt.WindowsEventLogSystemArtifactPath, MediaType: receipt.WindowsEventLogSystemArtifactMedia, ContentSchemaID: receipt.WindowsEventLogSystemArtifactSchema, RawOrDerived: "RAW", Size: 1, SHA256: strings.Repeat("b", 64)}
	invalid["process with Event Log artifact"] = wrongProcessArtifact
	wrongEventArtifact := event
	wrongEventArtifact.ArtifactReference = &receipt.ArtifactReference{Path: receipt.FirstStageArtifactPath, MediaType: receipt.FirstStageArtifactMedia, ContentSchemaID: receipt.FirstStageArtifactSchema, RawOrDerived: "DERIVED", Size: 1, SHA256: strings.Repeat("a", 64)}
	invalid["Event Log with process artifact"] = wrongEventArtifact
	fabricatedCancelled := eventCancelledReceipt()
	fabricatedCancelled.ArtifactReference = event.ArtifactReference
	invalid["cancelled Event Log with artifact"] = fabricatedCancelled
	for name, record := range invalid {
		if err := record.Validate(); err == nil {
			t.Errorf("invalid %s fixture unexpectedly accepted", name)
		}
	}
}

func TestFirstStageV1RuntimePackageReferenceParity(t *testing.T) {
	process := validMultiProcessReceipt()
	event := validMultiEventReceipt()
	metadata := validMultiMetadata(process, event)
	if err := metadata.Validate(); err != nil {
		t.Fatalf("valid complete multi package rejected by runtime: %v", err)
	}

	partial := metadata
	partial.RunState = "PARTIAL"
	partial.OrchestrationReason = "CANCELLED"
	partial.ArtifactReferences = []receipt.ArtifactReference{*process.ArtifactReference}
	if err := partial.Validate(); err != nil {
		t.Fatalf("valid process-retained/Event Log-cancelled package rejected: %v", err)
	}

	duplicate := metadata
	duplicate.ArtifactReferences = append([]receipt.ArtifactReference(nil), metadata.ArtifactReferences...)
	duplicate.ArtifactReferences[1] = duplicate.ArtifactReferences[0]
	if err := duplicate.Validate(); err == nil {
		t.Fatal("duplicate artifact ownership unexpectedly accepted")
	}

	duplicateReceipts := metadata
	duplicateReceipts.ReceiptReferences = []string{receipt.FirstStageReceiptPath(capability.ProcessIdentitySnapshotID), receipt.FirstStageReceiptPath(capability.ProcessIdentitySnapshotID)}
	if err := duplicateReceipts.Validate(); err == nil {
		t.Fatal("duplicate capability receipt ownership unexpectedly accepted")
	}
}

func readSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return schema
}

func assertConstAt(t *testing.T, defs map[string]any, definition, property string, want string) {
	t.Helper()
	value, ok := defs[definition].(map[string]any)
	if !ok {
		t.Fatalf("definition %s is not an object", definition)
	}
	allOf, ok := value["allOf"].([]any)
	if !ok {
		t.Fatalf("definition %s has no allOf", definition)
	}
	for _, branch := range allOf {
		branchMap, ok := branch.(map[string]any)
		if !ok {
			continue
		}
		properties, ok := branchMap["properties"].(map[string]any)
		if !ok {
			continue
		}
		propertyMap, ok := properties[property].(map[string]any)
		if ok && propertyMap["const"] == want {
			return
		}
	}
	t.Fatalf("definition %s does not bind %s to %q", definition, property, want)
}

func validMultiProcessReceipt() receipt.FirstStageRecord {
	definition := capability.ProcessIdentitySnapshot()
	reason := execution.ReasonNone
	start := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	return receipt.FirstStageRecord{
		SchemaVersion: receipt.FirstStageMultiSchemaVersion, ManifestVersion: receipt.ManifestVersion, ProductVersion: receipt.ProductVersion, RuntimeArtifact: receipt.FirstStageMultiRuntimeArtifact,
		CollectionID: "COL-00000000-0000-4000-8000-000000000001", TargetFingerprint: testFingerprint(),
		RequestedCapability: capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityNormal, Protected: true}, Capability: &definition,
		SelectedProvider: &receipt.ProviderIdentity{ID: "windows-toolhelp-process-snapshot", Class: provider.FirstPartyNative}, Compatibility: execution.Available, CompatibilityReason: &reason,
		CandidateEvaluations: []resolver.CandidateEvaluation{{ProviderID: "windows-toolhelp-process-snapshot", Compatibility: execution.Available, Reason: execution.ReasonNone, Eligible: true}}, Attempted: true,
		Execution:            execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "test"},
		ArtifactReference:    &receipt.ArtifactReference{Path: receipt.FirstStageArtifactPath, MediaType: receipt.FirstStageArtifactMedia, ContentSchemaID: receipt.FirstStageArtifactSchema, RawOrDerived: "DERIVED", Size: 1, SHA256: strings.Repeat("a", 64)},
		AcquisitionStartedAt: start.Format(time.RFC3339Nano), AcquisitionFinishedAt: start.Add(time.Second).Format(time.RFC3339Nano),
	}
}

func validMultiEventReceipt() receipt.FirstStageRecord {
	record := validMultiProcessReceipt()
	definition := capability.WindowsEventLogSystemChannel()
	reason := execution.ReasonNone
	record.RequestedCapability = capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityLate, Protected: false}
	record.Capability = &definition
	record.SelectedProvider = &receipt.ProviderIdentity{ID: "windows-wevtapi-system-channel", Class: provider.FirstPartyNative}
	record.CompatibilityReason = &reason
	record.CandidateEvaluations = []resolver.CandidateEvaluation{{ProviderID: "windows-wevtapi-system-channel", Compatibility: execution.Available, Reason: execution.ReasonNone, Eligible: true}}
	record.ArtifactReference = &receipt.ArtifactReference{Path: receipt.WindowsEventLogSystemArtifactPath, MediaType: receipt.WindowsEventLogSystemArtifactMedia, ContentSchemaID: receipt.WindowsEventLogSystemArtifactSchema, RawOrDerived: "RAW", Size: 1, SHA256: strings.Repeat("b", 64)}
	return record
}

func eventUnavailableReceipt() receipt.FirstStageRecord {
	record := validMultiEventReceipt()
	reason := execution.ReasonAPIUnavailable
	record.SelectedProvider = nil
	record.Compatibility = execution.Unavailable
	record.CompatibilityReason = &reason
	record.CandidateEvaluations = nil
	record.Attempted = false
	record.Execution = execution.Result{State: execution.Skipped, Reason: execution.ReasonAPIUnavailable, SideEffectSummary: "No provider was executed."}
	record.ArtifactReference = nil
	return record
}

func eventCancelledReceipt() receipt.FirstStageRecord {
	record := validMultiEventReceipt()
	record.Attempted = false
	record.Execution = execution.Result{State: execution.Skipped, Reason: execution.ReasonNone, SideEffectSummary: "No provider was executed."}
	record.OrchestrationReason = "CANCELLED"
	record.ArtifactReference = nil
	return record
}

func validMultiMetadata(process, event receipt.FirstStageRecord) receipt.FirstStagePackageMetadata {
	return receipt.FirstStagePackageMetadata{
		SchemaVersion: receipt.FirstStageMultiSchemaVersion, ManifestVersion: receipt.ManifestVersion, ProductVersion: receipt.ProductVersion, RuntimeArtifact: receipt.FirstStageMultiRuntimeArtifact,
		CollectionID: process.CollectionID, StartedAt: process.AcquisitionStartedAt, FinishedAt: process.AcquisitionFinishedAt, TargetFingerprint: process.TargetFingerprint, RunState: "COMPLETE",
		ReceiptReferences: []string{receipt.FirstStageReceiptPath(capability.ProcessIdentitySnapshotID), receipt.FirstStageReceiptPath(capability.WindowsEventLogSystemChannelID)}, ArtifactReferences: []receipt.ArtifactReference{*process.ArtifactReference, *event.ArtifactReference},
		DirectoryLayout: []string{"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports"},
	}
}

func testFingerprint() fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "synthetic", Build: "0", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN"}
}
