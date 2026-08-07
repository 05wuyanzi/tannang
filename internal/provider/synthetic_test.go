// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider_test

import (
	"context"
	"testing"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/provider"
)

func TestRequiredFixturesLoad(t *testing.T) {
	t.Parallel()
	names := []string{
		"available-collected",
		"degraded-partial",
		"unavailable",
		"privilege-blocked",
		"provider-failure",
	}
	for _, name := range names {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture, err := provider.LoadFixture(name)
			if err != nil {
				t.Fatalf("LoadFixture(%q) error: %v", name, err)
			}
			if fixture.Name != name {
				t.Fatalf("fixture name = %q, want %q", fixture.Name, name)
			}
		})
	}
}

func TestFixtureNameBoundary(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../available-collected", "folder/name", `folder\name`, "", "Available"} {
		if _, err := provider.LoadFixture(name); err == nil {
			t.Fatalf("LoadFixture(%q) unexpectedly succeeded", name)
		}
	}
}

func TestSyntheticRunnerBlocksActiveTrace(t *testing.T) {
	t.Parallel()
	fixture, err := provider.LoadFixture("available-collected")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := provider.NewSyntheticRunner(fixture, fixture.Providers[0])
	if err != nil {
		t.Fatal(err)
	}
	request := fixture.Capability
	request.AcquisitionSemantics = capability.ActiveTrace
	result := runner.Execute(context.Background(), request, fixture.Target)
	if result.State != execution.Blocked || result.Reason != execution.ReasonPolicyDisabled {
		t.Fatalf("Active Trace result = %+v, want BLOCKED/POLICY_DISABLED", result)
	}
}
