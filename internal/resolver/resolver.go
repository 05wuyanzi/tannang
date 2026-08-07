// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package resolver

import (
	"errors"
	"sort"
	"strings"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
)

// Policy controls side-effect classes independently of provider class.
type Policy struct {
	AllowActiveTrace bool
}

// CandidateEvaluation records why each candidate was or was not eligible.
type CandidateEvaluation struct {
	ProviderID    string                       `json:"provider_id"`
	Compatibility execution.CompatibilityState `json:"compatibility"`
	Reason        execution.Reason             `json:"reason"`
	Eligible      bool                         `json:"eligible"`
	Score         int                          `json:"score"`
}

// Decision separates compatibility from the later execution result.
type Decision struct {
	Selected      *provider.Descriptor         `json:"selected,omitempty"`
	Compatibility execution.CompatibilityState `json:"compatibility"`
	Reason        execution.Reason             `json:"reason"`
	Evaluations   []CandidateEvaluation        `json:"evaluations"`
}

// Resolve evaluates declarations without executing a provider.
func Resolve(
	request capability.Capability,
	target fingerprint.TargetFingerprint,
	candidates []provider.Descriptor,
	policy Policy,
) (Decision, error) {
	if err := request.Validate(); err != nil {
		return Decision{}, err
	}
	if err := target.Validate(); err != nil {
		return Decision{}, err
	}
	if request.AcquisitionSemantics == capability.ActiveTrace && !policy.AllowActiveTrace {
		return Decision{
			Compatibility: execution.Unavailable,
			Reason:        execution.ReasonPolicyDisabled,
			Evaluations:   []CandidateEvaluation{},
		}, nil
	}

	ordered := append([]provider.Descriptor(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	decision := Decision{
		Compatibility: execution.Unavailable,
		Reason:        execution.ReasonDependencyMissing,
		Evaluations:   make([]CandidateEvaluation, 0, len(ordered)),
	}
	bestScore := -1 << 30
	for _, descriptor := range ordered {
		if err := descriptor.Validate(); err != nil {
			return Decision{}, err
		}
		evaluation := evaluate(request, target, descriptor)
		decision.Evaluations = append(decision.Evaluations, evaluation)
		if !evaluation.Eligible {
			continue
		}
		if decision.Selected == nil || evaluation.Score > bestScore {
			selected := descriptor
			decision.Selected = &selected
			decision.Compatibility = evaluation.Compatibility
			decision.Reason = evaluation.Reason
			bestScore = evaluation.Score
		}
	}

	if decision.Selected == nil && len(decision.Evaluations) > 0 {
		decision.Reason = decision.Evaluations[0].Reason
		if decision.Reason == execution.ReasonNone {
			decision.Reason = execution.ReasonDependencyMissing
		}
	}
	return decision, nil
}

func evaluate(
	request capability.Capability,
	target fingerprint.TargetFingerprint,
	descriptor provider.Descriptor,
) CandidateEvaluation {
	evaluation := CandidateEvaluation{
		ProviderID:    descriptor.ID,
		Compatibility: execution.Unavailable,
		Reason:        execution.ReasonDependencyMissing,
		Eligible:      false,
	}
	if !descriptor.Supports(request.ID) {
		return evaluation
	}
	if !descriptor.Requirements.Available {
		evaluation.Reason = descriptor.Requirements.AvailabilityReason
		if evaluation.Reason == execution.ReasonNone {
			evaluation.Reason = execution.ReasonDependencyMissing
		}
		return evaluation
	}
	if !matches(descriptor.Requirements.Platforms, target.Platform) ||
		!matches(descriptor.Requirements.OSFamilies, target.OSFamily) ||
		!matches(descriptor.Requirements.RuntimeLanes, target.RuntimeLane) {
		evaluation.Reason = execution.ReasonUnsupportedOS
		return evaluation
	}
	if !matches(descriptor.Requirements.Architectures, target.Architecture) {
		evaluation.Reason = execution.ReasonUnsupportedArch
		return evaluation
	}

	evaluation.Eligible = true
	evaluation.Score = score(descriptor)
	if descriptor.Requirements.RequiresElevation && !target.Elevated {
		evaluation.Compatibility = execution.Degraded
		evaluation.Reason = execution.ReasonPrivilegeRequired
		return evaluation
	}
	evaluation.Compatibility = descriptor.Quality.Compatibility
	evaluation.Reason = descriptor.Quality.Reason
	if evaluation.Compatibility == execution.Unavailable {
		evaluation.Eligible = false
	}
	return evaluation
}

func matches(allowed []string, observed string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if strings.EqualFold(value, observed) {
			return true
		}
	}
	return false
}

func score(descriptor provider.Descriptor) int {
	quality := descriptor.Quality
	return quality.Fidelity*4 +
		quality.Completeness*4 +
		quality.OutputStability*2 +
		quality.EvidenceValue*3 -
		quality.Disturbance*3
}

// ValidateDecision checks invariants before execution.
func ValidateDecision(decision Decision) error {
	if !decision.Compatibility.Valid() {
		return errors.New("resolver produced invalid compatibility state")
	}
	if !decision.Reason.Valid() {
		return errors.New("resolver produced invalid reason")
	}
	if decision.Selected == nil && decision.Compatibility != execution.Unavailable {
		return errors.New("resolver returned compatible state without a selected provider")
	}
	if decision.Selected != nil && decision.Compatibility == execution.Unavailable {
		return errors.New("resolver selected an unavailable provider")
	}
	return nil
}
