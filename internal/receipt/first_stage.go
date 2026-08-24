// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package receipt

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

const (
	FirstStageRuntimeArtifact           = "tannang-first-stage"
	FirstStageMultiRuntimeArtifact      = "tannang-first-stage-multi"
	FirstStageProviderID                = "windows-toolhelp-process-snapshot"
	FirstStageArtifactPath              = "derived/process-identity-snapshot.ndjson"
	FirstStageArtifactMedia             = "application/x-ndjson"
	FirstStageArtifactSchema            = "https://github.com/05wuyanzi/tannang/contracts/process-identity-snapshot-record-v0.schema.json"
	WindowsEventLogSystemArtifactPath   = "raw/windows-event-log-system.evtx"
	WindowsEventLogSystemArtifactMedia  = "application/x-evtx"
	WindowsEventLogSystemArtifactSchema = "urn:tannang:artifact:windows-event-log-system-evtx-v0"
	WindowsEventLogSystemMediaType      = WindowsEventLogSystemArtifactMedia
	WindowsEventLogSystemSchemaID       = WindowsEventLogSystemArtifactSchema
	FirstStageMultiSchemaVersion        = "1.1"
)

// ArtifactReference is the package-layer identity of one known real artifact.
// Provider descriptors intentionally do not carry this classification.
type ArtifactReference struct {
	Path            string `json:"path"`
	MediaType       string `json:"media_type"`
	ContentSchemaID string `json:"content_schema_id"`
	RawOrDerived    string `json:"raw_or_derived"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
}

func (r ArtifactReference) Validate() error {
	if r.Path != FirstStageArtifactPath || r.MediaType != FirstStageArtifactMedia || r.ContentSchemaID != FirstStageArtifactSchema {
		return errors.New("first-stage artifact identity is invalid")
	}
	if r.RawOrDerived != "DERIVED" {
		return errors.New("first-stage artifact must be DERIVED")
	}
	if r.Size < 0 || len(r.SHA256) != 64 {
		return errors.New("first-stage artifact size or SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(r.SHA256); err != nil || strings.ToLower(r.SHA256) != r.SHA256 {
		return errors.New("first-stage artifact SHA-256 must be lowercase hexadecimal")
	}
	return nil
}

// ValidateForCapability validates one of the two fixed real artifact
// identities. Validate remains the v1.0 process-only authority.
func (r ArtifactReference) ValidateForCapability(capabilityID string) error {
	switch capabilityID {
	case capability.ProcessIdentitySnapshotID:
		return r.Validate()
	case capability.WindowsEventLogSystemChannelID:
		if r.Path != WindowsEventLogSystemArtifactPath || r.MediaType != WindowsEventLogSystemArtifactMedia || r.ContentSchemaID != WindowsEventLogSystemArtifactSchema || r.RawOrDerived != "RAW" {
			return errors.New("Windows Event Log artifact identity is invalid")
		}
		return validateArtifactDigest(r)
	default:
		return errors.New("unsupported first-stage artifact capability")
	}
}

func validateArtifactDigest(r ArtifactReference) error {
	if r.Size < 0 || len(r.SHA256) != 64 {
		return errors.New("first-stage artifact size or SHA-256 is invalid")
	}
	if _, err := hex.DecodeString(r.SHA256); err != nil || strings.ToLower(r.SHA256) != r.SHA256 {
		return errors.New("first-stage artifact SHA-256 must be lowercase hexadecimal")
	}
	return nil
}

// FirstStageRecord is the real Provider receipt contract. Execution.Payload is
// deliberately forbidden; artifact bytes live only in the package artifact.
type FirstStageRecord struct {
	SchemaVersion         string                         `json:"schema_version"`
	ManifestVersion       string                         `json:"manifest_version"`
	ProductVersion        string                         `json:"product_version"`
	RuntimeArtifact       string                         `json:"runtime_artifact"`
	CollectionID          string                         `json:"collection_id"`
	CaseID                string                         `json:"case_id,omitempty"`
	TargetFingerprint     fingerprint.TargetFingerprint  `json:"target_fingerprint"`
	RequestedCapability   capability.CapabilityRequest   `json:"requested_capability"`
	Capability            *capability.Capability         `json:"capability,omitempty"`
	SelectedProvider      *ProviderIdentity              `json:"selected_provider,omitempty"`
	Compatibility         execution.CompatibilityState   `json:"compatibility"`
	CompatibilityReason   *execution.Reason              `json:"compatibility_reason,omitempty"`
	CandidateEvaluations  []resolver.CandidateEvaluation `json:"candidate_evaluations,omitempty"`
	Attempted             bool                           `json:"attempted"`
	Execution             execution.Result               `json:"execution"`
	OrchestrationReason   string                         `json:"orchestration_reason,omitempty"`
	MissingEvidence       []string                       `json:"missing_evidence,omitempty"`
	ArtifactReference     *ArtifactReference             `json:"artifact_reference,omitempty"`
	AcquisitionStartedAt  string                         `json:"acquisition_started_at"`
	AcquisitionFinishedAt string                         `json:"acquisition_finished_at"`
}

func (r FirstStageRecord) Validate() error {
	if r.SchemaVersion == FirstStageMultiSchemaVersion {
		return r.validateMulti()
	}
	if r.SchemaVersion != SchemaVersion || r.ManifestVersion != ManifestVersion || r.ProductVersion == "" || r.RuntimeArtifact != FirstStageRuntimeArtifact {
		return errors.New("unsupported first-stage receipt identity")
	}
	if strings.TrimSpace(r.CollectionID) == "" {
		return errors.New("first-stage collection id is required")
	}
	if err := r.TargetFingerprint.Validate(); err != nil {
		return fmt.Errorf("validate first-stage receipt fingerprint: %w", err)
	}
	if err := r.RequestedCapability.Validate(); err != nil {
		return fmt.Errorf("validate first-stage request: %w", err)
	}
	if r.Capability != nil {
		if err := r.Capability.Validate(); err != nil {
			return fmt.Errorf("validate first-stage capability: %w", err)
		}
		if *r.Capability != capability.ProcessIdentitySnapshot() {
			return errors.New("first-stage capability does not match the fixed process identity snapshot definition")
		}
		if r.Capability.ID != r.RequestedCapability.ID {
			return errors.New("first-stage capability does not match request")
		}
	}
	if !r.Compatibility.Valid() {
		return errors.New("first-stage compatibility is invalid")
	}
	if r.SelectedProvider != nil {
		if r.SelectedProvider.ID != FirstStageProviderID || r.SelectedProvider.Class != provider.FirstPartyNative {
			return errors.New("first-stage selected provider is invalid")
		}
		if r.Capability == nil {
			return errors.New("first-stage selected provider requires a known capability")
		}
	}
	if r.SelectedProvider == nil && r.Compatibility != execution.Unavailable {
		return errors.New("missing selected provider requires unavailable compatibility")
	}
	if r.SelectedProvider != nil && r.Compatibility == execution.Unavailable {
		return errors.New("selected first-stage provider cannot have unavailable compatibility")
	}
	if r.CompatibilityReason != nil && !r.CompatibilityReason.Valid() {
		return errors.New("first-stage compatibility reason is invalid")
	}
	if len(r.CandidateEvaluations) > 1 {
		return errors.New("first-stage receipt allows at most one candidate evaluation")
	}
	for _, evaluation := range r.CandidateEvaluations {
		if evaluation.ProviderID != FirstStageProviderID || !evaluation.Compatibility.Valid() || !evaluation.Reason.Valid() {
			return errors.New("first-stage candidate evaluation is invalid")
		}
	}
	if r.SelectedProvider != nil {
		matched := false
		for _, evaluation := range r.CandidateEvaluations {
			if evaluation.ProviderID == r.SelectedProvider.ID {
				if evaluation.Compatibility != r.Compatibility || r.CompatibilityReason == nil || evaluation.Reason != *r.CompatibilityReason || !evaluation.Eligible {
					return errors.New("selected provider evaluation is inconsistent with receipt compatibility")
				}
				matched = true
			}
		}
		if !matched {
			return errors.New("selected provider is missing its candidate evaluation")
		}
	}
	if err := r.Execution.Validate(); err != nil {
		return fmt.Errorf("validate first-stage execution: %w", err)
	}
	if len(r.Execution.Payload) != 0 {
		return errors.New("first-stage receipt payload must be empty")
	}
	if !r.Attempted && r.Execution.State != execution.Skipped {
		return errors.New("non-attempted first-stage receipt must use SKIPPED execution")
	}
	if r.Attempted {
		if r.Capability == nil || r.SelectedProvider == nil || r.CompatibilityReason == nil || len(r.CandidateEvaluations) != 1 {
			return errors.New("attempted first-stage receipt requires capability, provider, decision reason, and evaluations")
		}
	}
	if r.OrchestrationReason != "" && r.Attempted {
		return errors.New("first-stage orchestration reason requires non-attempted accounting")
	}
	if r.OrchestrationReason != "" && r.Execution.Reason != execution.ReasonNone {
		return errors.New("first-stage orchestration reason requires NONE execution reason")
	}
	switch r.OrchestrationReason {
	case "", "UNKNOWN_CAPABILITY", "RESOLUTION_FAILED", "CANCELLED", "PACKAGE_FINALIZATION_FAILED":
	default:
		return errors.New("first-stage orchestration reason is invalid")
	}
	if r.OrchestrationReason == "PACKAGE_FINALIZATION_FAILED" {
		if r.Attempted || r.Execution.State != execution.Skipped || r.Execution.Reason != execution.ReasonNone || r.SelectedProvider == nil || r.Capability == nil || r.CompatibilityReason == nil || len(r.CandidateEvaluations) != 1 || len(r.MissingEvidence) != 1 || r.MissingEvidence[0] != FirstStagePackagePreparationMissingEvidence || r.ArtifactReference != nil {
			return errors.New("package-preparation failure receipt shape is invalid")
		}
	}
	if r.ArtifactReference != nil {
		if !r.Attempted || (r.Execution.State != execution.Collected && r.Execution.State != execution.Partial) {
			return errors.New("first-stage artifact reference requires a retainable execution")
		}
		if err := r.ArtifactReference.Validate(); err != nil {
			return err
		}
	} else if r.Execution.State == execution.Collected || r.Execution.State == execution.Partial {
		return errors.New("retainable first-stage execution requires an artifact reference")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.AcquisitionStartedAt); err != nil {
		return fmt.Errorf("invalid first-stage acquisition start: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, r.AcquisitionFinishedAt); err != nil {
		return fmt.Errorf("invalid first-stage acquisition finish: %w", err)
	}
	return nil
}

func (r FirstStageRecord) validateMulti() error {
	if r.ManifestVersion != ManifestVersion || r.ProductVersion == "" || r.RuntimeArtifact != FirstStageMultiRuntimeArtifact {
		return errors.New("unsupported multi-artifact receipt identity")
	}
	if strings.TrimSpace(r.CollectionID) == "" {
		return errors.New("first-stage collection id is required")
	}
	if err := r.TargetFingerprint.Validate(); err != nil {
		return fmt.Errorf("validate first-stage receipt fingerprint: %w", err)
	}
	if err := r.RequestedCapability.Validate(); err != nil {
		return fmt.Errorf("validate first-stage request: %w", err)
	}
	if r.RequestedCapability.ID != capability.ProcessIdentitySnapshotID && r.RequestedCapability.ID != capability.WindowsEventLogSystemChannelID {
		return errors.New("multi-artifact receipt contains an unknown capability")
	}
	if r.RequestedCapability.ID == capability.WindowsEventLogSystemChannelID && r.RequestedCapability.Protected {
		return errors.New("Event Log supplemental request must not be protected")
	}
	if r.Capability == nil {
		return errors.New("multi-artifact receipt capability is required")
	}
	if err := r.Capability.Validate(); err != nil || r.Capability.ID != r.RequestedCapability.ID {
		return errors.New("multi-artifact capability does not match request")
	}
	if r.RequestedCapability.ID == capability.ProcessIdentitySnapshotID && *r.Capability != capability.ProcessIdentitySnapshot() {
		return errors.New("multi-artifact process capability definition is invalid")
	}
	if r.RequestedCapability.ID == capability.WindowsEventLogSystemChannelID && *r.Capability != capability.WindowsEventLogSystemChannel() {
		return errors.New("multi-artifact Event Log capability definition is invalid")
	}
	if !r.Compatibility.Valid() {
		return errors.New("multi-artifact compatibility is invalid")
	}
	if r.SelectedProvider != nil {
		if r.SelectedProvider.Class != provider.FirstPartyNative || (r.RequestedCapability.ID == capability.ProcessIdentitySnapshotID && r.SelectedProvider.ID != FirstStageProviderID) || (r.RequestedCapability.ID == capability.WindowsEventLogSystemChannelID && r.SelectedProvider.ID != "windows-wevtapi-system-channel") {
			return errors.New("multi-artifact selected provider is invalid")
		}
	} else if r.Compatibility != execution.Unavailable {
		return errors.New("missing selected provider requires unavailable compatibility")
	}
	if r.CompatibilityReason != nil && !r.CompatibilityReason.Valid() {
		return errors.New("multi-artifact compatibility reason is invalid")
	}
	for _, evaluation := range r.CandidateEvaluations {
		if !evaluation.Compatibility.Valid() || !evaluation.Reason.Valid() || strings.TrimSpace(evaluation.ProviderID) == "" {
			return errors.New("multi-artifact candidate evaluation is invalid")
		}
	}
	if r.SelectedProvider != nil {
		matched := false
		for _, evaluation := range r.CandidateEvaluations {
			if evaluation.ProviderID == r.SelectedProvider.ID {
				if !evaluation.Eligible || r.CompatibilityReason == nil || evaluation.Compatibility != r.Compatibility || evaluation.Reason != *r.CompatibilityReason {
					return errors.New("selected multi-artifact provider evaluation is inconsistent")
				}
				matched = true
			}
		}
		if !matched {
			return errors.New("selected multi-artifact provider is missing its candidate evaluation")
		}
	}
	if err := r.Execution.Validate(); err != nil {
		return fmt.Errorf("validate multi-artifact execution: %w", err)
	}
	if len(r.Execution.Payload) != 0 {
		return errors.New("multi-artifact receipt payload must be empty")
	}
	if !r.Attempted && r.Execution.State != execution.Skipped {
		return errors.New("non-attempted multi-artifact receipt must use SKIPPED execution")
	}
	if r.OrchestrationReason != "" && (r.Attempted || r.Execution.Reason != execution.ReasonNone) {
		return errors.New("multi-artifact orchestration reason requires non-attempted NONE accounting")
	}
	if r.ArtifactReference != nil {
		if !r.Attempted || (r.Execution.State != execution.Collected && r.Execution.State != execution.Partial) {
			return errors.New("multi-artifact reference requires retainable execution")
		}
		if err := r.ArtifactReference.ValidateForCapability(r.RequestedCapability.ID); err != nil {
			return err
		}
	} else if r.Execution.State == execution.Collected || r.Execution.State == execution.Partial {
		return errors.New("retainable multi-artifact execution requires a reference")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.AcquisitionStartedAt); err != nil {
		return fmt.Errorf("invalid first-stage acquisition start: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, r.AcquisitionFinishedAt); err != nil {
		return fmt.Errorf("invalid first-stage acquisition finish: %w", err)
	}
	return nil
}

// FirstStagePackageMetadata is the fixed package-level summary written before
// manifest generation. It contains references, never a duplicate artifact.
type FirstStagePackageMetadata struct {
	SchemaVersion       string                        `json:"schema_version"`
	ManifestVersion     string                        `json:"manifest_version"`
	ProductVersion      string                        `json:"product_version"`
	RuntimeArtifact     string                        `json:"runtime_artifact"`
	CollectionID        string                        `json:"collection_id"`
	CaseID              string                        `json:"case_id,omitempty"`
	StartedAt           string                        `json:"started_at"`
	FinishedAt          string                        `json:"finished_at"`
	TargetFingerprint   fingerprint.TargetFingerprint `json:"target_fingerprint"`
	RunState            string                        `json:"run_state"`
	OrchestrationReason string                        `json:"orchestration_reason,omitempty"`
	ReceiptReferences   []string                      `json:"receipt_references"`
	ArtifactReferences  []ArtifactReference           `json:"artifact_references"`
	DirectoryLayout     []string                      `json:"directory_layout"`
}

func (m FirstStagePackageMetadata) Validate() error {
	if m.SchemaVersion == FirstStageMultiSchemaVersion {
		return m.validateMulti()
	}
	if m.SchemaVersion != SchemaVersion || m.ManifestVersion != ManifestVersion || m.ProductVersion == "" || m.RuntimeArtifact != FirstStageRuntimeArtifact {
		return errors.New("unsupported first-stage package metadata identity")
	}
	if strings.TrimSpace(m.CollectionID) == "" || (m.RunState != "COMPLETE" && m.RunState != "PARTIAL") {
		return errors.New("first-stage package metadata identity is incomplete")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.StartedAt); err != nil {
		return fmt.Errorf("invalid first-stage package start: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, m.FinishedAt); err != nil {
		return fmt.Errorf("invalid first-stage package finish: %w", err)
	}
	if err := m.TargetFingerprint.Validate(); err != nil {
		return fmt.Errorf("validate first-stage package fingerprint: %w", err)
	}
	if len(m.ReceiptReferences) == 0 {
		return errors.New("first-stage package requires receipt references")
	}
	seen := make(map[string]struct{}, len(m.ReceiptReferences))
	for _, ref := range m.ReceiptReferences {
		if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") {
			return errors.New("first-stage receipt reference is unsafe")
		}
		if _, ok := seen[ref]; ok {
			return errors.New("duplicate first-stage receipt reference")
		}
		seen[ref] = struct{}{}
	}
	if m.ArtifactReferences == nil {
		return errors.New("first-stage package artifact references must be an array")
	}
	if len(m.ArtifactReferences) > 1 {
		return errors.New("first-stage package allows at most one artifact reference")
	}
	for _, ref := range m.ArtifactReferences {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	if len(m.DirectoryLayout) == 0 {
		return errors.New("first-stage package directory layout is required")
	}
	return nil
}

func (m FirstStagePackageMetadata) validateMulti() error {
	if m.ManifestVersion != ManifestVersion || m.ProductVersion == "" || m.RuntimeArtifact != FirstStageMultiRuntimeArtifact {
		return errors.New("unsupported multi-artifact package metadata identity")
	}
	if strings.TrimSpace(m.CollectionID) == "" || (m.RunState != "COMPLETE" && m.RunState != "PARTIAL") {
		return errors.New("multi-artifact package metadata identity is incomplete")
	}
	if _, err := time.Parse(time.RFC3339Nano, m.StartedAt); err != nil {
		return fmt.Errorf("invalid package start: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, m.FinishedAt); err != nil {
		return fmt.Errorf("invalid package finish: %w", err)
	}
	if err := m.TargetFingerprint.Validate(); err != nil {
		return fmt.Errorf("validate package fingerprint: %w", err)
	}
	if len(m.ReceiptReferences) == 0 {
		return errors.New("multi-artifact package requires receipt references")
	}
	seen := make(map[string]struct{}, len(m.ReceiptReferences))
	for _, ref := range m.ReceiptReferences {
		if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "/") || strings.Contains(ref, "..") {
			return errors.New("unsafe receipt reference")
		}
		if _, ok := seen[ref]; ok {
			return errors.New("duplicate receipt reference")
		}
		seen[ref] = struct{}{}
	}
	if m.ArtifactReferences == nil || len(m.ArtifactReferences) > 2 {
		return errors.New("multi-artifact package artifact references must contain zero to two entries")
	}
	seenPaths := make(map[string]struct{}, len(m.ArtifactReferences))
	for _, ref := range m.ArtifactReferences {
		if err := ref.ValidateForCapability(capabilityForArtifactPath(ref.Path)); err != nil {
			return err
		}
		if _, ok := seenPaths[ref.Path]; ok {
			return errors.New("duplicate artifact reference")
		}
		seenPaths[ref.Path] = struct{}{}
	}
	if len(m.DirectoryLayout) == 0 {
		return errors.New("multi-artifact package directory layout is required")
	}
	return nil
}

func capabilityForArtifactPath(path string) string {
	if path == WindowsEventLogSystemArtifactPath {
		return capability.WindowsEventLogSystemChannelID
	}
	return capability.ProcessIdentitySnapshotID
}

const FirstStagePackagePreparationMissingEvidence = "The requested evidence is absent because package staging preparation failed before Provider execution."

func FirstStageReceiptPath(capabilityID string) string {
	return "receipts/" + capabilityID + ".json"
}
