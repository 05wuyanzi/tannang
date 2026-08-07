// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package application coordinates the synthetic genesis execution path.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/05wuyanzi/tannang/internal/evidence"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/receipt"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

// Outcome records a completed orchestration attempt, including non-successful
// provider states whose evidence packages were still created.
type Outcome struct {
	PackagePath string         `json:"package_path"`
	Record      receipt.Record `json:"record"`
}

// Collect executes only a named embedded synthetic fixture.
func Collect(ctx context.Context, fixtureName, output string) (Outcome, error) {
	fixture, err := provider.LoadFixture(fixtureName)
	if err != nil {
		return Outcome{}, err
	}
	started := time.Now().UTC()
	decision, err := resolver.Resolve(
		fixture.Capability,
		fixture.Target,
		fixture.Providers,
		resolver.Policy{AllowActiveTrace: false},
	)
	if err != nil {
		return Outcome{}, fmt.Errorf("resolve synthetic provider: %w", err)
	}
	if err := resolver.ValidateDecision(decision); err != nil {
		return Outcome{}, fmt.Errorf("validate resolver decision: %w", err)
	}

	result := execution.Result{
		State:  execution.Skipped,
		Reason: decision.Reason,
		Detail: "No compatible synthetic provider was selected.",
	}
	var selected *receipt.ProviderIdentity
	var providerClass provider.Class
	var payload json.RawMessage
	if decision.Selected != nil {
		runner, err := provider.NewSyntheticRunner(fixture, *decision.Selected)
		if err != nil {
			return Outcome{}, fmt.Errorf("bind synthetic provider: %w", err)
		}
		result = runner.Execute(ctx, fixture.Capability, fixture.Target)
		payload = result.Payload
		selected = &receipt.ProviderIdentity{ID: decision.Selected.ID, Class: decision.Selected.Class}
		providerClass = decision.Selected.Class
	}
	finished := time.Now().UTC()
	artifactPath := ""
	if len(payload) > 0 && (result.State == execution.Collected || result.State == execution.Partial) {
		if fixture.Behavior.RawOrDerived == "RAW" {
			artifactPath = "raw/synthetic-result.json"
		} else {
			artifactPath = "derived/synthetic-result.json"
		}
	}
	record := receipt.Record{
		SchemaVersion:        receipt.SchemaVersion,
		ManifestVersion:      receipt.ManifestVersion,
		ProductVersion:       receipt.ProductVersion,
		RuntimeArtifact:      receipt.RuntimeArtifact,
		RuntimeLane:          fixture.Target.RuntimeLane,
		FixtureName:          fixture.Name,
		TargetFingerprint:    fixture.Target,
		RequestedCapability:  fixture.Capability,
		SelectedProvider:     selected,
		ProviderClass:        providerClass,
		Compatibility:        decision.Compatibility,
		CompatibilityReason:  decision.Reason,
		Execution:            result,
		Reason:               result.Reason,
		AcquisitionSemantics: fixture.Capability.AcquisitionSemantics,
		RawOrDerived:         fixture.Behavior.RawOrDerived,
		ArtifactPath:         artifactPath,
		StartedAt:            started.Format(time.RFC3339Nano),
		FinishedAt:           finished.Format(time.RFC3339Nano),
		SideEffectSummary:    fixture.Behavior.SideEffectSummary,
		CandidateEvaluations: decision.Evaluations,
	}
	if err := evidence.Create(output, record, payload); err != nil {
		return Outcome{}, fmt.Errorf("create evidence package: %w", err)
	}
	record.DirectoryLayout = []string{"meta", "raw", "derived", "normalized", "receipts", "hashes", "handoff", "reports"}
	return Outcome{PackagePath: output, Record: record}, nil
}
