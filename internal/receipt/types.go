// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package receipt defines the stable record joining resolution and execution.
package receipt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

const (
	SchemaVersion   = "1.0"
	ManifestVersion = "1.0"
	ProductVersion  = "0.0.0-pre-alpha"
	RuntimeArtifact = "tannang-genesis-synthetic"
)

// ProviderIdentity records the selected implementation without making it the
// user-facing capability.
type ProviderIdentity struct {
	ID    string         `json:"id"`
	Class provider.Class `json:"class"`
}

// Record is written into both package metadata and the execution receipt.
type Record struct {
	SchemaVersion        string                          `json:"schema_version"`
	ManifestVersion      string                          `json:"manifest_version"`
	ProductVersion       string                          `json:"product_version"`
	RuntimeArtifact      string                          `json:"runtime_artifact"`
	RuntimeLane          string                          `json:"runtime_lane"`
	FixtureName          string                          `json:"fixture_name"`
	TargetFingerprint    fingerprint.TargetFingerprint   `json:"target_fingerprint"`
	RequestedCapability  capability.Capability           `json:"requested_capability"`
	SelectedProvider     *ProviderIdentity               `json:"selected_provider"`
	ProviderClass        provider.Class                  `json:"provider_class"`
	Compatibility        execution.CompatibilityState    `json:"compatibility"`
	CompatibilityReason  execution.Reason                `json:"compatibility_reason"`
	Execution            execution.Result                `json:"execution"`
	Reason               execution.Reason                `json:"reason"`
	AcquisitionSemantics capability.AcquisitionSemantics `json:"acquisition_semantics"`
	RawOrDerived         string                          `json:"raw_or_derived"`
	ArtifactPath         string                          `json:"artifact_path,omitempty"`
	StartedAt            string                          `json:"started_at"`
	FinishedAt           string                          `json:"finished_at"`
	SideEffectSummary    string                          `json:"side_effect_summary"`
	CandidateEvaluations []resolver.CandidateEvaluation  `json:"candidate_evaluations"`
	DirectoryLayout      []string                        `json:"directory_layout"`
}

// Validate enforces state separation and required package facts.
func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion || r.ManifestVersion != ManifestVersion {
		return errors.New("unsupported receipt schema or manifest version")
	}
	if r.ProductVersion == "" || r.RuntimeArtifact == "" || r.RuntimeLane == "" {
		return errors.New("product version, runtime artifact, and runtime lane are required")
	}
	if strings.TrimSpace(r.FixtureName) == "" {
		return errors.New("fixture name is required")
	}
	if err := r.TargetFingerprint.Validate(); err != nil {
		return fmt.Errorf("validate receipt target fingerprint: %w", err)
	}
	if err := r.RequestedCapability.Validate(); err != nil {
		return fmt.Errorf("validate receipt capability: %w", err)
	}
	if !r.Compatibility.Valid() || !r.CompatibilityReason.Valid() {
		return errors.New("receipt compatibility state or reason is invalid")
	}
	if !r.Execution.State.Valid() || !r.Execution.Reason.Valid() || !r.Reason.Valid() {
		return errors.New("receipt execution state or reason is invalid")
	}
	if r.Reason != r.Execution.Reason {
		return errors.New("top-level reason must match execution reason")
	}
	if r.AcquisitionSemantics != r.RequestedCapability.AcquisitionSemantics {
		return errors.New("receipt acquisition semantics do not match the capability")
	}
	if r.RawOrDerived != "RAW" && r.RawOrDerived != "DERIVED" {
		return errors.New("receipt raw_or_derived must be RAW or DERIVED")
	}
	if strings.TrimSpace(r.StartedAt) == "" || strings.TrimSpace(r.FinishedAt) == "" {
		return errors.New("receipt start and finish times are required")
	}
	if strings.TrimSpace(r.SideEffectSummary) == "" {
		return errors.New("receipt side effect summary is required")
	}
	if r.SelectedProvider == nil {
		if r.Compatibility != execution.Unavailable || r.ProviderClass != "" {
			return errors.New("missing selected provider requires unavailable compatibility and empty provider class")
		}
	} else if r.SelectedProvider.ID == "" || !r.SelectedProvider.Class.Valid() || r.ProviderClass != r.SelectedProvider.Class {
		return errors.New("selected provider identity is invalid")
	}
	if len(r.DirectoryLayout) == 0 {
		return errors.New("evidence package directory layout is required")
	}
	return nil
}
