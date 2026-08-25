// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package receipt

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/buildinfo"
	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

func TestFirstStageRecordValidation(t *testing.T) {
	record := validFirstStageReceipt()
	if err := record.Validate(); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	for name, mutate := range map[string]func(*FirstStageRecord){
		"payload":              func(value *FirstStageRecord) { value.Execution.Payload = json.RawMessage(`{"forbidden":true}`) },
		"wrong classification": func(value *FirstStageRecord) { value.ArtifactReference.RawOrDerived = "RAW" },
		"bad hash":             func(value *FirstStageRecord) { value.ArtifactReference.SHA256 = strings.Repeat("g", 64) },
		"negative size":        func(value *FirstStageRecord) { value.ArtifactReference.Size = -1 },
		"artifact on failed": func(value *FirstStageRecord) {
			value.Execution.State = execution.Failed
			value.Execution.Reason = execution.ReasonProviderError
		},
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			candidate := record
			artifact := *record.ArtifactReference
			candidate.ArtifactReference = &artifact
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatalf("invalid receipt unexpectedly validated: %+v", candidate)
			}
		})
	}
	missingIdentity := record
	missingIdentity.Capability = nil
	if err := missingIdentity.Validate(); err == nil {
		t.Fatal("attempted receipt without capability unexpectedly validated")
	}
	missingIdentity = record
	missingIdentity.SelectedProvider = nil
	if err := missingIdentity.Validate(); err == nil {
		t.Fatal("attempted receipt without provider unexpectedly validated")
	}
	missingIdentity = record
	missingIdentity.CandidateEvaluations = nil
	if err := missingIdentity.Validate(); err == nil {
		t.Fatal("attempted receipt without decision evaluations unexpectedly validated")
	}
	wrongRequest := record
	wrongRequest.RequestedCapability.ID = "OTHER_CAPABILITY"
	if err := wrongRequest.Validate(); err == nil {
		t.Fatal("receipt outside the fixed capability unexpectedly validated")
	}
	wrongDefinition := record
	capabilityCopy := *record.Capability
	capabilityCopy.Description = "other definition"
	wrongDefinition.Capability = &capabilityCopy
	if err := wrongDefinition.Validate(); err == nil {
		t.Fatal("receipt with a mismatched capability definition unexpectedly validated")
	}
	duplicateDecision := record
	duplicateDecision.CandidateEvaluations = append(duplicateDecision.CandidateEvaluations, duplicateDecision.CandidateEvaluations[0])
	if err := duplicateDecision.Validate(); err == nil {
		t.Fatal("receipt with duplicate decision evaluations unexpectedly validated")
	}
}

func TestFirstStageUnavailableReceiptPreservesResolverReason(t *testing.T) {
	record := validFirstStageReceipt()
	record.SelectedProvider = nil
	record.Compatibility = execution.Unavailable
	record.CompatibilityReason = func() *execution.Reason { value := execution.ReasonAPIUnavailable; return &value }()
	record.CandidateEvaluations = nil
	record.Attempted = false
	record.Execution = execution.Result{State: execution.Skipped, Reason: execution.ReasonAPIUnavailable, SideEffectSummary: "No provider was executed."}
	record.MissingEvidence = []string{"No suitable provider was available."}
	record.ArtifactReference = nil
	if err := record.Validate(); err != nil {
		t.Fatalf("valid unavailable receipt rejected: %v", err)
	}
	record.OrchestrationReason = "CANCELLED"
	if err := record.Validate(); err == nil {
		t.Fatal("application-owned reason accepted a non-NONE execution reason")
	}
}

func TestFirstStageUnknownSupplementalReceiptRemainsValid(t *testing.T) {
	record := validFirstStageReceipt()
	record.RequestedCapability = capability.CapabilityRequest{ID: "UNKNOWN_LATE", Priority: capability.PriorityLate}
	record.Capability = nil
	record.SelectedProvider = nil
	record.Compatibility = execution.Unavailable
	record.CompatibilityReason = nil
	record.CandidateEvaluations = nil
	record.Attempted = false
	record.Execution = execution.Result{State: execution.Skipped, Reason: execution.ReasonNone, SideEffectSummary: "No provider was executed."}
	record.OrchestrationReason = "UNKNOWN_CAPABILITY"
	record.MissingEvidence = []string{"The requested capability is not present in the trusted catalog."}
	record.ArtifactReference = nil
	if err := record.Validate(); err != nil {
		t.Fatalf("valid unknown supplemental receipt rejected: %v", err)
	}
}

func TestPreAttemptPackageFailureReceiptRequiresExactEvidence(t *testing.T) {
	record := validFirstStageReceipt()
	record.Attempted = false
	record.Execution = execution.Result{State: execution.Skipped, Reason: execution.ReasonNone, SideEffectSummary: "No provider was executed."}
	record.OrchestrationReason = "PACKAGE_FINALIZATION_FAILED"
	record.ArtifactReference = nil
	record.MissingEvidence = []string{FirstStagePackagePreparationMissingEvidence}
	if err := record.Validate(); err != nil {
		t.Fatalf("valid pre-attempt package failure rejected: %v", err)
	}
	for _, evidence := range [][]string{nil, {}, {"arbitrary"}, {FirstStagePackagePreparationMissingEvidence, "extra"}} {
		candidate := record
		candidate.MissingEvidence = evidence
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid package failure evidence unexpectedly validated: %#v", evidence)
		}
	}
	providerMismatch := record
	providerMismatch.SelectedProvider = &ProviderIdentity{ID: "other-provider", Class: provider.FirstPartyNative}
	if err := providerMismatch.Validate(); err == nil {
		t.Fatal("pre-attempt provider mismatch unexpectedly validated")
	}
	decisionMismatch := record
	decisionMismatch.CandidateEvaluations = []resolver.CandidateEvaluation{{ProviderID: FirstStageProviderID, Compatibility: execution.Degraded, Reason: execution.ReasonPrivilegeRequired, Eligible: true}}
	if err := decisionMismatch.Validate(); err == nil {
		t.Fatal("pre-attempt decision mismatch unexpectedly validated")
	}
	ineligibleDecision := record
	ineligibleDecision.CandidateEvaluations[0].Eligible = false
	if err := ineligibleDecision.Validate(); err == nil {
		t.Fatal("pre-attempt ineligible selected decision unexpectedly validated")
	}
}

func TestFirstStagePackageMetadataValidation(t *testing.T) {
	record := validFirstStageReceipt()
	metadata := validFirstStagePackageMetadata(record)
	if err := metadata.Validate(); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	receiptOnly := metadata
	receiptOnly.ArtifactReferences = []ArtifactReference{}
	if err := receiptOnly.Validate(); err != nil {
		t.Fatalf("empty artifact array rejected: %v", err)
	}
	receiptOnly.ArtifactReferences = nil
	if err := receiptOnly.Validate(); err == nil {
		t.Fatal("nil artifact references unexpectedly validated")
	}
	metadata.ReceiptReferences = append(metadata.ReceiptReferences, metadata.ReceiptReferences[0])
	if err := metadata.Validate(); err == nil {
		t.Fatal("duplicate receipt references unexpectedly validated")
	}
	metadata.ReceiptReferences = metadata.ReceiptReferences[:1]
	metadata.FinishedAt = "invalid"
	if err := metadata.Validate(); err == nil {
		t.Fatal("invalid package timestamp unexpectedly validated")
	}
}

func TestFirstStageBuildProductVersionFormsRemainValid(t *testing.T) {
	revision := strings.Repeat("a", 40)
	versions := map[string]string{
		"clean":    buildinfo.BaseVersion + "+git." + revision,
		"modified": buildinfo.BaseVersion + "+git." + revision + ".modified",
		"unknown":  buildinfo.BaseVersion + "+source.unknown",
	}
	for name, productVersion := range versions {
		t.Run(name, func(t *testing.T) {
			record := validFirstStageReceipt()
			record.ProductVersion = productVersion
			metadata := validFirstStagePackageMetadata(record)
			metadata.ProductVersion = productVersion
			if err := record.Validate(); err != nil {
				t.Fatalf("receipt rejected product version %q: %v", productVersion, err)
			}
			if err := metadata.Validate(); err != nil {
				t.Fatalf("metadata rejected product version %q: %v", productVersion, err)
			}
			if metadata.ProductVersion != record.ProductVersion {
				t.Fatalf("metadata product version %q differs from receipt %q", metadata.ProductVersion, record.ProductVersion)
			}
		})
	}
}

func TestFirstStageMultiEventLogReceiptAndMetadataValidate(t *testing.T) {
	for _, protected := range []bool{false, true} {
		protected := protected
		t.Run(fmt.Sprintf("protected=%t", protected), func(t *testing.T) {
			record := validFirstStageReceipt()
			definition := capability.WindowsEventLogSystemChannel()
			reason := execution.ReasonNone
			record.SchemaVersion = FirstStageMultiSchemaVersion
			record.RuntimeArtifact = FirstStageMultiRuntimeArtifact
			record.RequestedCapability = capability.CapabilityRequest{ID: capability.WindowsEventLogSystemChannelID, Priority: capability.PriorityLate, Protected: protected}
			record.Capability = &definition
			record.SelectedProvider = &ProviderIdentity{ID: "windows-wevtapi-system-channel", Class: provider.FirstPartyNative}
			record.CompatibilityReason = &reason
			record.CandidateEvaluations = []resolver.CandidateEvaluation{{ProviderID: "windows-wevtapi-system-channel", Compatibility: execution.Available, Reason: execution.ReasonNone, Eligible: true}}
			record.ArtifactReference = &ArtifactReference{Path: WindowsEventLogSystemArtifactPath, MediaType: WindowsEventLogSystemArtifactMedia, ContentSchemaID: WindowsEventLogSystemArtifactSchema, RawOrDerived: "RAW", Size: 16, SHA256: strings.Repeat("b", 64)}
			if err := record.Validate(); err != nil {
				t.Fatalf("multi Event Log receipt rejected: %v", err)
			}
			metadata := validFirstStagePackageMetadata(record)
			metadata.SchemaVersion = FirstStageMultiSchemaVersion
			metadata.RuntimeArtifact = FirstStageMultiRuntimeArtifact
			metadata.ArtifactReferences = []ArtifactReference{*record.ArtifactReference}
			if err := metadata.Validate(); err != nil {
				t.Fatalf("multi Event Log metadata rejected: %v", err)
			}
		})
	}
}

func validFirstStagePackageMetadata(record FirstStageRecord) FirstStagePackageMetadata {
	return FirstStagePackageMetadata{
		SchemaVersion: SchemaVersion, ManifestVersion: ManifestVersion, ProductVersion: ProductVersion, RuntimeArtifact: FirstStageRuntimeArtifact,
		CollectionID: record.CollectionID, StartedAt: record.AcquisitionStartedAt, FinishedAt: record.AcquisitionFinishedAt, TargetFingerprint: record.TargetFingerprint, RunState: "COMPLETE",
		ReceiptReferences: []string{FirstStageReceiptPath(capability.ProcessIdentitySnapshotID)}, ArtifactReferences: []ArtifactReference{*record.ArtifactReference},
		DirectoryLayout: []string{"meta", "derived", "receipts", "hashes", "handoff"},
	}
}

func validFirstStageReceipt() FirstStageRecord {
	definition := capability.ProcessIdentitySnapshot()
	request := capability.CapabilityRequest{ID: definition.ID, Priority: capability.PriorityNormal, Protected: true}
	reason := execution.ReasonNone
	start := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	return FirstStageRecord{
		SchemaVersion: SchemaVersion, ManifestVersion: ManifestVersion, ProductVersion: ProductVersion, RuntimeArtifact: FirstStageRuntimeArtifact,
		CollectionID: "COL-00000000-0000-4000-8000-000000000001", TargetFingerprint: fingerprint.TargetFingerprint{
			Platform: "windows", OSFamily: "WindowsNT", Version: "synthetic", Build: "0", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "MODERN",
		},
		RequestedCapability: request, Capability: &definition,
		SelectedProvider: &ProviderIdentity{ID: "windows-toolhelp-process-snapshot", Class: provider.FirstPartyNative},
		Compatibility:    execution.Available, CompatibilityReason: &reason,
		CandidateEvaluations: []resolver.CandidateEvaluation{{ProviderID: "windows-toolhelp-process-snapshot", Compatibility: execution.Available, Reason: execution.ReasonNone, Eligible: true}},
		Attempted:            true, Execution: execution.Result{State: execution.Collected, Reason: execution.ReasonNone, SideEffectSummary: "Fixed receipt test execution."},
		ArtifactReference:    &ArtifactReference{Path: FirstStageArtifactPath, MediaType: FirstStageArtifactMedia, ContentSchemaID: FirstStageArtifactSchema, RawOrDerived: "DERIVED", Size: 1, SHA256: strings.Repeat("a", 64)},
		AcquisitionStartedAt: start.Format(time.RFC3339Nano), AcquisitionFinishedAt: start.Add(time.Second).Format(time.RFC3339Nano),
	}
}
