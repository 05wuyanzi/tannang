// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package resolver_test

import (
	"testing"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
	"github.com/05wuyanzi/tannang/internal/provider"
	"github.com/05wuyanzi/tannang/internal/resolver"
)

func TestActiveTracePolicyDenied(t *testing.T) {
	t.Parallel()
	request := testCapability()
	request.AcquisitionSemantics = capability.ActiveTrace
	decision, err := resolver.Resolve(request, testTarget(), []provider.Descriptor{testDescriptor("candidate", provider.SyntheticTest, 5)}, resolver.Policy{})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if decision.Selected != nil || decision.Compatibility != execution.Unavailable || decision.Reason != execution.ReasonPolicyDisabled {
		t.Fatalf("unexpected Active Trace decision: %+v", decision)
	}
}

func TestProviderClassHasNoPermanentPriority(t *testing.T) {
	t.Parallel()
	candidates := []provider.Descriptor{
		testDescriptor("a-inbox", provider.WindowsInbox, 1),
		testDescriptor("z-external", provider.ExternalBackend, 5),
	}
	decision, err := resolver.Resolve(testCapability(), testTarget(), candidates, resolver.Policy{})
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if decision.Selected == nil || decision.Selected.ID != "z-external" {
		t.Fatalf("selected provider = %+v, want z-external", decision.Selected)
	}
}

func testCapability() capability.Capability {
	return capability.Capability{
		ID:                   "SYNTHETIC_TEST",
		Description:          "Synthetic resolver test.",
		AcquisitionSemantics: capability.StateSnapshot,
		Sensitivity:          "low",
	}
}

func testTarget() fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{
		Platform:     "windows",
		OSFamily:     "WindowsNT",
		Version:      "synthetic",
		Build:        "0",
		Architecture: "amd64",
		Privilege:    "standard-user",
		RuntimeLane:  "MODERN",
	}
}

func testDescriptor(id string, class provider.Class, quality int) provider.Descriptor {
	return provider.Descriptor{
		ID:           id,
		Class:        class,
		Capabilities: []string{"SYNTHETIC_TEST"},
		Requirements: provider.Requirements{
			Available:          true,
			AvailabilityReason: execution.ReasonNone,
		},
		SideEffects: []string{"none"},
		Quality: provider.Quality{
			Compatibility:   execution.Available,
			Reason:          execution.ReasonNone,
			Fidelity:        quality,
			Completeness:    quality,
			OutputStability: quality,
			EvidenceValue:   quality,
		},
	}
}
